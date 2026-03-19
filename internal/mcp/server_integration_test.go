package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/types"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	gomcp "github.com/mark3labs/mcp-go/mcp"
)

func TestServerStdioIntegrationWithMCPClient(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)

	repoRoot := repositoryRoot(t)
	workspaceDir := t.TempDir()
	filePath := filepath.Join(workspaceDir, "main.go")
	configPath := filepath.Join(workspaceDir, "config.json")

	writeFile(t, filepath.Join(workspaceDir, "go.mod"), "module example.com/mcpintegration\n\ngo 1.22\n")
	writeFile(t, filePath, `package main

func helper() {}

func main() {
	helper()
}
`)
	writeFile(t, configPath, `{
  "languages": [
    {
      "name": "go",
      "extensions": [".go"],
      "languageId": "go",
      "command": "gopls",
      "args": []
    }
  ]
}`)

	client, stderrBuf := newTestMCPClient(t, ctx, repoRoot, configPath)
	t.Cleanup(func() {
		cancel()
	})

	initResult, err := client.Initialize(ctx, gomcp.InitializeRequest{
		Params: gomcp.InitializeParams{
			ProtocolVersion: gomcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: gomcp.Implementation{
				Name:    "bergo-lsp-mcp-integration-test",
				Version: "1.0.0",
			},
			Capabilities: gomcp.ClientCapabilities{},
		},
	})
	if err != nil {
		t.Fatalf("initialize client: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if initResult.ServerInfo.Name != "bergo-lsp-mcp" {
		t.Fatalf("unexpected server name: %q", initResult.ServerInfo.Name)
	}

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping server: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}

	toolsResult, err := client.ListTools(ctx, gomcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}

	var toolNames []string
	for _, tool := range toolsResult.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	for _, want := range []string{"find_definition", "find_references", "file_outline"} {
		if !slices.Contains(toolNames, want) {
			t.Fatalf("tool %q not exposed; got %v", want, toolNames)
		}
	}

	rootURI := "file://" + filepath.ToSlash(workspaceDir)

	definitionResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "find_definition",
			Arguments: map[string]any{
				"filePath":   filePath,
				"rootUri":    rootURI,
				"line":       6,
				"symbolName": "helper",
			},
		},
	})
	if err != nil {
		t.Fatalf("call find_definition: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if definitionResult.IsError {
		t.Fatalf("find_definition returned error result: %+v", definitionResult)
	}

	var definition types.QueryResult
	decodeStructuredContent(t, definitionResult.StructuredContent, &definition)
	if len(definition.Items) == 0 {
		t.Fatal("find_definition returned no items")
	}
	if definition.Items[0].FilePath != filePath || definition.Items[0].Line != 3 {
		t.Fatalf("unexpected definition result: %+v", definition.Items[0])
	}

	referencesResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "find_references",
			Arguments: map[string]any{
				"filePath":   filePath,
				"rootUri":    rootURI,
				"line":       6,
				"symbolName": "helper",
			},
		},
	})
	if err != nil {
		t.Fatalf("call find_references: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if referencesResult.IsError {
		t.Fatalf("find_references returned error result: %+v", referencesResult)
	}

	var references types.QueryResult
	decodeStructuredContent(t, referencesResult.StructuredContent, &references)
	if len(references.Items) == 0 {
		t.Fatal("find_references returned no items")
	}
	if !hasLine(references.Items, 6) {
		t.Fatalf("find_references did not include call site; got %+v", references.Items)
	}

	outlineResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "file_outline",
			Arguments: map[string]any{
				"filePath": filePath,
				"rootUri":  rootURI,
			},
		},
	})
	if err != nil {
		t.Fatalf("call file_outline: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if outlineResult.IsError {
		t.Fatalf("file_outline returned error result: %+v", outlineResult)
	}

	var outline types.OutlineResult
	decodeStructuredContent(t, outlineResult.StructuredContent, &outline)
	if len(outline.Items) == 0 {
		t.Fatal("file_outline returned no items")
	}
	if !hasOutlineName(outline.Items, "helper") || !hasOutlineName(outline.Items, "main") {
		t.Fatalf("file_outline missing expected symbols; got %+v", outline.Items)
	}
}

func TestServerStdioIntegrationAgainstCurrentProject(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	repoRoot := repositoryRoot(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	targetFile := filepath.Join(repoRoot, "internal", "service", "service.go")

	writeGoConfig(t, configPath)

	client, stderrBuf := newTestMCPClient(t, ctx, repoRoot, configPath)
	t.Cleanup(func() {
		cancel()
	})

	_, err := client.Initialize(ctx, gomcp.InitializeRequest{
		Params: gomcp.InitializeParams{
			ProtocolVersion: gomcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: gomcp.Implementation{
				Name:    "bergo-lsp-mcp-self-integration-test",
				Version: "1.0.0",
			},
			Capabilities: gomcp.ClientCapabilities{},
		},
	})
	if err != nil {
		t.Fatalf("initialize client: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}

	rootURI := "file://" + filepath.ToSlash(repoRoot)
	callSiteLine := findLineContaining(t, targetFile, "prepareQuery(ctx, query.FilePath, query.RootURI, query.Line, query.SymbolName, query.Index)", 2)
	definitionLine := findLineContaining(t, targetFile, "func (s *Service) prepareQuery(", 1)

	definitionResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "find_definition",
			Arguments: map[string]any{
				"filePath":   targetFile,
				"rootUri":    rootURI,
				"line":       callSiteLine,
				"symbolName": "prepareQuery",
			},
		},
	})
	if err != nil {
		t.Fatalf("call find_definition: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if definitionResult.IsError {
		t.Fatalf("find_definition returned error result: %+v", definitionResult)
	}

	var definition types.QueryResult
	decodeStructuredContent(t, definitionResult.StructuredContent, &definition)
	if len(definition.Items) == 0 {
		t.Fatal("find_definition returned no items")
	}
	if !hasLine(definition.Items, definitionLine) {
		t.Fatalf("find_definition did not include prepareQuery definition line %d; got %+v", definitionLine, definition.Items)
	}

	referencesResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "find_references",
			Arguments: map[string]any{
				"filePath":   targetFile,
				"rootUri":    rootURI,
				"line":       definitionLine,
				"symbolName": "prepareQuery",
			},
		},
	})
	if err != nil {
		t.Fatalf("call find_references: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if referencesResult.IsError {
		t.Fatalf("find_references returned error result: %+v", referencesResult)
	}

	var references types.QueryResult
	decodeStructuredContent(t, referencesResult.StructuredContent, &references)
	if len(references.Items) < 2 {
		t.Fatalf("find_references returned too few items: %+v", references.Items)
	}
	if !hasLine(references.Items, callSiteLine) || !hasLine(references.Items, definitionLine) {
		t.Fatalf("find_references missing expected lines %d/%d; got %+v", callSiteLine, definitionLine, references.Items)
	}

	outlineResult, err := client.CallTool(ctx, gomcp.CallToolRequest{
		Params: gomcp.CallToolParams{
			Name: "file_outline",
			Arguments: map[string]any{
				"filePath": targetFile,
				"rootUri":  rootURI,
			},
		},
	})
	if err != nil {
		t.Fatalf("call file_outline: %v\nserver stderr:\n%s", err, stderrBuf.String())
	}
	if outlineResult.IsError {
		t.Fatalf("file_outline returned error result: %+v", outlineResult)
	}

	var outline types.OutlineResult
	decodeStructuredContent(t, outlineResult.StructuredContent, &outline)
	if len(outline.Items) == 0 {
		t.Fatal("file_outline returned no items")
	}
	if !hasOutlineName(outline.Items, "(*Service).FindDefinition") || !hasOutlineName(outline.Items, "(*Service).prepareQuery") {
		t.Fatalf("file_outline missing expected symbols; got %+v", outline.Items)
	}
}

func newTestMCPClient(t *testing.T, ctx context.Context, repoRoot string, configPath string) (*mcpclient.Client, *safeBuffer) {
	t.Helper()

	stderrBuf := &safeBuffer{}
	client, err := mcpclient.NewStdioMCPClientWithOptions(
		"go",
		[]string{"BERGO_LSP_MCP_CONFIG=" + configPath},
		[]string{"run", "."},
		transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(), env...)
			return cmd, nil
		}),
	)
	if err != nil {
		t.Fatalf("start mcp client: %v", err)
	}

	if stderr, ok := mcpclient.GetStderr(client); ok {
		go func() {
			_, _ = io.Copy(stderrBuf, stderr)
		}()
	}

	return client, stderrBuf
}

func decodeStructuredContent(t *testing.T, v any, target any) {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal structured content: %v; payload=%s", err, data)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeGoConfig(t *testing.T, path string) {
	t.Helper()

	writeFile(t, path, `{
  "languages": [
    {
      "name": "go",
      "extensions": [".go"],
      "languageId": "go",
      "command": "gopls",
      "args": []
    }
  ]
}`)
}

func hasLine(items []types.Position, line int) bool {
	for _, item := range items {
		if item.Line == line {
			return true
		}
	}
	return false
}

func findLineContaining(t *testing.T, path string, needle string, occurrence int) int {
	t.Helper()

	if occurrence <= 0 {
		t.Fatalf("occurrence must be >= 1, got %d", occurrence)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	count := 0
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			count++
			if count == occurrence {
				return i + 1
			}
		}
	}

	t.Fatalf("could not find occurrence %d of %q in %s", occurrence, needle, path)
	return 0
}

func hasOutlineName(items []types.OutlineItem, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
