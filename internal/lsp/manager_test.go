package lsp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/config"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

type fakeTransportFactory struct {
	server func(*fakeLSPServer) jsonrpc2.Handler
}

func (f fakeTransportFactory) Start(ctx context.Context, spec LaunchSpec) (Transport, error) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	clientSide := &pipeTransport{reader: serverReader, writer: serverWriter}
	serverSide := &pipeTransport{reader: clientReader, writer: clientWriter}

	conn := jsonrpc2.NewConn(jsonrpc2.NewStream(serverSide))
	server := &fakeLSPServer{}
	go conn.Go(ctx, f.server(server))
	return clientSide, nil
}

type pipeTransport struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *pipeTransport) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p *pipeTransport) Write(b []byte) (int, error) { return p.writer.Write(b) }
func (p *pipeTransport) Close() error {
	_ = p.writer.Close()
	return p.reader.Close()
}
func (p *pipeTransport) Process() Process { return fakeProcess{} }

type fakeProcess struct{}

func (fakeProcess) Wait() error { return nil }
func (fakeProcess) Kill() error { return nil }

type fakeLSPServer struct {
	mu          sync.Mutex
	openCount   int
	changeCount int
}

func (f *fakeLSPServer) handler() jsonrpc2.Handler {
	return func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		switch req.Method() {
		case protocol.MethodInitialize:
			result := protocol.InitializeResult{
				Capabilities: protocol.ServerCapabilities{
					ReferencesProvider:     true,
					DefinitionProvider:     true,
					DocumentSymbolProvider: true,
					TextDocumentSync: protocol.TextDocumentSyncOptions{
						OpenClose: true,
						Change:    protocol.TextDocumentSyncKindFull,
					},
				},
			}
			return reply(ctx, result, nil)
		case protocol.MethodInitialized:
			return reply(ctx, nil, nil)
		case protocol.MethodTextDocumentDidOpen:
			f.mu.Lock()
			f.openCount++
			f.mu.Unlock()
			return reply(ctx, nil, nil)
		case protocol.MethodTextDocumentDidChange:
			f.mu.Lock()
			f.changeCount++
			f.mu.Unlock()
			return reply(ctx, nil, nil)
		case protocol.MethodShutdown:
			return reply(ctx, nil, nil)
		case protocol.MethodExit:
			return nil
		default:
			return reply(ctx, nil, nil)
		}
	}
}

func TestManagerReusesClientAndSyncs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := &config.Config{
		Languages: []config.LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "fake"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	fakeServer := &fakeLSPServer{}
	manager := NewManagerWithFactory(cfg, fakeTransportFactory{
		server: func(_ *fakeLSPServer) jsonrpc2.Handler { return fakeServer.handler() },
	})
	t.Cleanup(func() { _ = manager.Close() })

	client1, lang, err := manager.ClientForFile(context.Background(), filePath, "")
	if err != nil {
		t.Fatalf("client for file: %v", err)
	}
	if err := client1.EnsureSynced(context.Background(), filePath, lang.LanguageID); err != nil {
		t.Fatalf("ensure synced: %v", err)
	}

	client2, _, err := manager.ClientForFile(context.Background(), filePath, "")
	if err != nil {
		t.Fatalf("client for file second call: %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected client reuse")
	}

	if err := os.WriteFile(filePath, []byte("package main\nfunc main() { println(\"x\") }\n"), 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	if err := client1.EnsureSynced(context.Background(), filePath, lang.LanguageID); err != nil {
		t.Fatalf("ensure synced second time: %v", err)
	}

	fakeServer.mu.Lock()
	defer fakeServer.mu.Unlock()
	if fakeServer.openCount != 1 {
		t.Fatalf("unexpected didOpen count: %d", fakeServer.openCount)
	}
	if fakeServer.changeCount != 1 {
		t.Fatalf("unexpected didChange count: %d", fakeServer.changeCount)
	}
}

func TestManagerUsesExplicitRootForClientKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := &config.Config{
		Languages: []config.LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "fake"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	manager := NewManagerWithFactory(cfg, fakeTransportFactory{
		server: func(_ *fakeLSPServer) jsonrpc2.Handler { return (&fakeLSPServer{}).handler() },
	})
	t.Cleanup(func() { _ = manager.Close() })

	rootA := filepath.Join(tmpDir, "root-a")
	rootB := filepath.Join(tmpDir, "root-b")

	clientA, _, err := manager.ClientForFile(context.Background(), filePath, rootA)
	if err != nil {
		t.Fatalf("client with rootA: %v", err)
	}
	clientB, _, err := manager.ClientForFile(context.Background(), filePath, rootB)
	if err != nil {
		t.Fatalf("client with rootB: %v", err)
	}
	if clientA == clientB {
		t.Fatal("expected different clients for different explicit roots")
	}
}

func TestDecodeDefinitionResultSupportsLocationLink(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal([]any{
		protocol.LocationLink{
			TargetURI: protocol.URI("file:///tmp/main.go"),
			TargetRange: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 1},
				End:   protocol.Position{Line: 1, Character: 4},
			},
			TargetSelectionRange: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 1},
				End:   protocol.Position{Line: 1, Character: 4},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := decodeDefinitionResult(raw)
	if err != nil {
		t.Fatalf("decode definition result: %v", err)
	}
	if len(got.Links) != 1 {
		t.Fatalf("unexpected link count: %d", len(got.Links))
	}
}
