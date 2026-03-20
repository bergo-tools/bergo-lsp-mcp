package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/config"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	lspuri "go.lsp.dev/uri"
	"go.uber.org/zap"
)

type Manager struct {
	cfg       *config.Config
	transport TransportFactory

	mu      sync.Mutex
	clients map[string]*Client
}

type Client struct {
	lang      *config.LanguageLSP
	rootDir   string
	server    protocol.Server
	conn      jsonrpc2.Conn
	transport Transport
	process   Process
	caps      protocol.ServerCapabilities

	mu    sync.Mutex
	files map[string]syncedFile
}

type syncedFile struct {
	version int32
	content string
}

type DefinitionResult struct {
	Locations []protocol.Location
	Links     []protocol.LocationLink
}

func NewManager(cfg *config.Config) *Manager {
	return NewManagerWithFactory(cfg, CommandTransportFactory{})
}

func NewManagerWithFactory(cfg *config.Config, factory TransportFactory) *Manager {
	return &Manager{
		cfg:       cfg,
		transport: factory,
		clients:   make(map[string]*Client),
	}
}

func (m *Manager) ClientForFile(ctx context.Context, filePath string, rootURI string) (*Client, *config.LanguageLSP, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve absolute file path: %w", err)
	}

	lang, err := m.cfg.MatchLanguage(absPath)
	if err != nil {
		return nil, nil, err
	}
	rootDir, err := m.cfg.ResolveRoot(absPath, lang, rootURI)
	if err != nil {
		return nil, nil, err
	}

	key := lang.Name + "::" + rootDir

	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[key]; ok {
		return client, lang, nil
	}

	client, err := m.startClient(ctx, lang, rootDir)
	if err != nil {
		return nil, nil, err
	}
	m.clients[key] = client
	return client, lang, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for key, client := range m.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	m.clients = make(map[string]*Client)
	return errors.Join(errs...)
}

func (m *Manager) startClient(ctx context.Context, lang *config.LanguageLSP, rootDir string) (*Client, error) {
	transport, err := m.transport.Start(ctx, LaunchSpec{
		Command: lang.Command,
		Args:    lang.Args,
		Env:     lang.Env,
		Dir:     rootDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start LSP server for %s: %w", lang.Name, err)
	}

	stream := jsonrpc2.NewStream(transport)
	clientProtocol := &noopClient{}
	_, conn, server := protocol.NewClient(ctx, clientProtocol, stream, zap.NewNop())

	result, err := server.Initialize(ctx, &protocol.InitializeParams{
		ProcessID: int32(os.Getpid()),
		RootURI:   protocol.URI(pathToURI(rootDir)),
		Capabilities: protocol.ClientCapabilities{
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Definition: &protocol.DefinitionTextDocumentClientCapabilities{
					LinkSupport: true,
				},
				DocumentSymbol: &protocol.DocumentSymbolClientCapabilities{
					HierarchicalDocumentSymbolSupport: true,
				},
			},
			Workspace: &protocol.WorkspaceClientCapabilities{
				WorkspaceFolders: true,
			},
		},
		WorkspaceFolders: []protocol.WorkspaceFolder{
			{
				URI:  pathToURI(rootDir),
				Name: filepath.Base(rootDir),
			},
		},
		InitializationOptions: lang.InitializationOptions,
	})
	if err != nil {
		_ = transport.Close()
		_ = transport.Process().Kill()
		return nil, fmt.Errorf("initialize LSP server for %s: %w", lang.Name, err)
	}
	if err := server.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		_ = transport.Close()
		_ = transport.Process().Kill()
		return nil, fmt.Errorf("send initialized for %s: %w", lang.Name, err)
	}

	return &Client{
		lang:      lang,
		rootDir:   rootDir,
		server:    server,
		conn:      conn,
		transport: transport,
		process:   transport.Process(),
		caps:      result.Capabilities,
		files:     make(map[string]syncedFile),
	}, nil
}

func (c *Client) RootDir() string {
	return c.rootDir
}

func (c *Client) EnsureSynced(ctx context.Context, filePath string, languageID string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve absolute file path: %w", err)
	}
	contentBytes, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file %q: %w", absPath, err)
	}
	content := string(contentBytes)
	uri := protocol.URI(pathToURI(absPath))

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists := c.files[absPath]
	if !exists {
		if err := c.server.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        uri,
				LanguageID: protocol.LanguageIdentifier(languageID),
				Version:    1,
				Text:       content,
			},
		}); err != nil {
			return fmt.Errorf("didOpen %q: %w", absPath, err)
		}
		c.files[absPath] = syncedFile{version: 1, content: content}
		return nil
	}

	if current.content == content {
		return nil
	}

	nextVersion := current.version + 1
	if err := c.server.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			Version: nextVersion,
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: uri,
			},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{Text: content},
		},
	}); err != nil {
		return fmt.Errorf("didChange %q: %w", absPath, err)
	}
	c.files[absPath] = syncedFile{version: nextVersion, content: content}
	return nil
}

func (c *Client) SupportsReferences() bool {
	return capabilityEnabled(c.caps.ReferencesProvider)
}

func (c *Client) SupportsDefinition() bool {
	return capabilityEnabled(c.caps.DefinitionProvider)
}

func (c *Client) SupportsImplementation() bool {
	return capabilityEnabled(c.caps.ImplementationProvider)
}

func (c *Client) SupportsRename() bool {
	return capabilityEnabled(c.caps.RenameProvider)
}

func (c *Client) SupportsDocumentSymbol() bool {
	return capabilityEnabled(c.caps.DocumentSymbolProvider)
}

func (c *Client) References(ctx context.Context, filePath string, lineZeroBased int, columnZeroBased int) ([]protocol.Location, error) {
	if !c.SupportsReferences() {
		return nil, errors.New("LSP server does not support references")
	}
	return c.server.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URI(pathToURI(filePath))},
			Position: protocol.Position{
				Line:      uint32(lineZeroBased),
				Character: uint32(columnZeroBased),
			},
		},
		Context: protocol.ReferenceContext{
			IncludeDeclaration: true,
		},
	})
}

func (c *Client) Definition(ctx context.Context, filePath string, lineZeroBased int, columnZeroBased int) (*DefinitionResult, error) {
	if !c.SupportsDefinition() {
		return nil, errors.New("LSP server does not support definition")
	}

	params := &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URI(pathToURI(filePath))},
			Position: protocol.Position{
				Line:      uint32(lineZeroBased),
				Character: uint32(columnZeroBased),
			},
		},
	}

	var raw json.RawMessage
	if err := protocol.Call(ctx, c.conn, protocol.MethodTextDocumentDefinition, params, &raw); err != nil {
		return nil, err
	}
	return decodeDefinitionResult(raw)
}

func (c *Client) Implementation(ctx context.Context, filePath string, lineZeroBased int, columnZeroBased int) (*DefinitionResult, error) {
	if !c.SupportsImplementation() {
		return nil, errors.New("LSP server does not support implementation")
	}

	params := &protocol.ImplementationParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URI(pathToURI(filePath))},
			Position: protocol.Position{
				Line:      uint32(lineZeroBased),
				Character: uint32(columnZeroBased),
			},
		},
	}

	var raw json.RawMessage
	if err := protocol.Call(ctx, c.conn, protocol.MethodTextDocumentImplementation, params, &raw); err != nil {
		return nil, err
	}
	return decodeDefinitionResult(raw)
}

func (c *Client) Rename(ctx context.Context, filePath string, lineZeroBased int, columnZeroBased int, newName string) (*protocol.WorkspaceEdit, error) {
	if !c.SupportsRename() {
		return nil, errors.New("LSP server does not support rename")
	}
	return c.server.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URI(pathToURI(filePath))},
			Position: protocol.Position{
				Line:      uint32(lineZeroBased),
				Character: uint32(columnZeroBased),
			},
		},
		NewName: newName,
	})
}

func (c *Client) DocumentSymbols(ctx context.Context, filePath string) ([]any, error) {
	if !c.SupportsDocumentSymbol() {
		return nil, errors.New("LSP server does not support document symbols")
	}

	var raw json.RawMessage
	if err := protocol.Call(ctx, c.conn, protocol.MethodTextDocumentDocumentSymbol, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URI(pathToURI(filePath))},
	}, &raw); err != nil {
		return nil, err
	}

	var items []json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode document symbols: %w", err)
	}

	out := make([]any, 0, len(items))
	for _, item := range items {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, fmt.Errorf("decode document symbol entry: %w", err)
		}
		if _, ok := probe["location"]; ok {
			var symbol protocol.SymbolInformation
			if err := json.Unmarshal(item, &symbol); err != nil {
				return nil, fmt.Errorf("decode symbol information: %w", err)
			}
			out = append(out, symbol)
			continue
		}

		var symbol protocol.DocumentSymbol
		if err := json.Unmarshal(item, &symbol); err != nil {
			return nil, fmt.Errorf("decode document symbol: %w", err)
		}
		out = append(out, symbol)
	}
	return out, nil
}

func (c *Client) Close() error {
	var errs []error
	if c.server != nil {
		if err := c.server.Shutdown(context.Background()); err != nil {
			errs = append(errs, fmt.Errorf("shutdown: %w", err))
		}
		if err := c.server.Exit(context.Background()); err != nil {
			errs = append(errs, fmt.Errorf("exit: %w", err))
		}
	}
	if c.transport != nil {
		if err := c.transport.Close(); err != nil {
			errs = append(errs, fmt.Errorf("transport close: %w", err))
		}
	}
	if c.process != nil {
		if err := c.process.Wait(); err != nil && !isExpectedExit(err) {
			errs = append(errs, fmt.Errorf("process wait: %w", err))
		}
	}
	return errors.Join(errs...)
}

func decodeDefinitionResult(raw json.RawMessage) (*DefinitionResult, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return &DefinitionResult{}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode definition result: %w", err)
	}
	result := &DefinitionResult{}
	for _, item := range items {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, fmt.Errorf("decode definition entry: %w", err)
		}
		if _, ok := probe["targetUri"]; ok {
			var link protocol.LocationLink
			if err := json.Unmarshal(item, &link); err != nil {
				return nil, fmt.Errorf("decode definition location link: %w", err)
			}
			result.Links = append(result.Links, link)
			continue
		}
		var loc protocol.Location
		if err := json.Unmarshal(item, &loc); err != nil {
			return nil, fmt.Errorf("decode definition location: %w", err)
		}
		result.Locations = append(result.Locations, loc)
	}
	return result, nil
}

func capabilityEnabled(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	default:
		return true
	}
}

func pathToURI(path string) string {
	return string(lspuri.File(path))
}

func URIToPath(uri protocol.URI) string {
	return lspuri.URI(uri).Filename()
}

func isExpectedExit(err error) bool {
	return err == nil || strings.Contains(err.Error(), "signal: killed") || strings.Contains(err.Error(), "exit status")
}

type noopClient struct{}

func (n *noopClient) Progress(context.Context, *protocol.ProgressParams) error { return nil }

func (n *noopClient) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

func (n *noopClient) LogMessage(context.Context, *protocol.LogMessageParams) error { return nil }

func (n *noopClient) PublishDiagnostics(context.Context, *protocol.PublishDiagnosticsParams) error {
	return nil
}

func (n *noopClient) ShowMessage(context.Context, *protocol.ShowMessageParams) error { return nil }

func (n *noopClient) ShowMessageRequest(context.Context, *protocol.ShowMessageRequestParams) (*protocol.MessageActionItem, error) {
	return nil, nil
}

func (n *noopClient) Telemetry(context.Context, interface{}) error { return nil }

func (n *noopClient) RegisterCapability(context.Context, *protocol.RegistrationParams) error {
	return nil
}

func (n *noopClient) UnregisterCapability(context.Context, *protocol.UnregistrationParams) error {
	return nil
}

func (n *noopClient) ApplyEdit(context.Context, *protocol.ApplyWorkspaceEditParams) (bool, error) {
	return true, nil
}

func (n *noopClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]interface{}, error) {
	return nil, nil
}

func (n *noopClient) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	return nil, nil
}
