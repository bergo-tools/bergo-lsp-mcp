package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/config"

	"go.lsp.dev/protocol"
)

func TestFlattenSymbols(t *testing.T) {
	t.Parallel()

	items := flattenSymbols("/tmp/main.go", []any{
		protocol.DocumentSymbol{
			Name: "MyType",
			Kind: protocol.SymbolKindClass,
			SelectionRange: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 0},
				End:   protocol.Position{Line: 1, Character: 6},
			},
			Children: []protocol.DocumentSymbol{
				{
					Name: "Method",
					Kind: protocol.SymbolKindMethod,
					SelectionRange: protocol.Range{
						Start: protocol.Position{Line: 3, Character: 1},
						End:   protocol.Position{Line: 3, Character: 7},
					},
				},
			},
		},
	})

	if len(items) != 2 {
		t.Fatalf("unexpected item count: %d", len(items))
	}
	if !strings.Contains(items[1], "MyType.Method") {
		t.Fatalf("unexpected item: %s", items[1])
	}
}

func TestApplyWorkspaceEdit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\nfunc helper() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	edit := &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			protocol.DocumentURI("file://" + filepath.ToSlash(filePath)): {
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 1, Character: 5},
						End:   protocol.Position{Line: 1, Character: 11},
					},
					NewText: "renamed",
				},
			},
		},
	}

	items, err := applyWorkspaceEdit(edit)
	if err != nil {
		t.Fatalf("apply workspace edit: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected item count: %d", len(items))
	}
	if !strings.Contains(items[0], filePath+":2: func renamed() {}") {
		t.Fatalf("unexpected edit: %s", items[0])
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "func renamed() {}") {
		t.Fatalf("rename was not applied: %s", string(data))
	}
}

func TestFilterLocationResultsFallbackWarning(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\nfunc example() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	items, warnings := filterLocationResults("MissingSymbol", []protocol.Location{
		{
			URI: protocol.URI("file://" + filepath.ToSlash(filePath)),
			Range: protocol.Range{
				Start: protocol.Position{Line: 1, Character: 0},
				End:   protocol.Position{Line: 1, Character: 4},
			},
		},
	})
	if len(items) != 1 {
		t.Fatalf("unexpected item count: %d", len(items))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warnings")
	}
}

func TestSymbolPositionRequiresIndexForAmbiguousLine(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("helper(helper)\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, _, err := symbolPosition(filePath, 1, "helper", 0)
	if err == nil {
		t.Fatal("expected ambiguous symbol error")
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"appears 2 times", "set index to 1-2"}) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSymbolPositionUsesRequestedIndex(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte("helper(helper)\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	lineZero, columnZero, warnings, err := symbolPosition(filePath, 1, "helper", 2)
	if err != nil {
		t.Fatalf("symbol position: %v", err)
	}
	if lineZero != 0 || columnZero != 7 {
		t.Fatalf("unexpected position: line=%d column=%d", lineZero, columnZero)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestIntegrationWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	tmpDir := t.TempDir()
	goMod := "module example.com/integration\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	source := `package main

func helper() {}

type Greeter interface {
	Hello()
}

type greeter struct{}

func (greeter) Hello() {}

func main() {
	helper()
	var g Greeter = greeter{}
	g.Hello()
}
`
	filePath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cfg := &config.Config{
		Languages: []config.LanguageLSP{
			{
				Name:       "go",
				Extensions: []string{".go"},
				LanguageID: "go",
				Command:    "gopls",
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	svc, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	helperCallLine := findLine(t, source, "\thelper()")
	interfaceHelloLine := findLine(t, source, "\tHello()")

	definition, err := svc.FindDefinition(context.Background(), DefinitionQuery{
		FilePath:   filePath,
		Line:       helperCallLine,
		SymbolName: "helper",
	})
	if err != nil {
		t.Fatalf("find definition: %v", err)
	}
	if len(definition.Items) == 0 {
		t.Fatal("expected definition results")
	}

	implementation, err := svc.FindImplementation(context.Background(), ImplementationQuery{
		FilePath:   filePath,
		Line:       interfaceHelloLine,
		SymbolName: "Hello",
	})
	if err != nil {
		t.Fatalf("find implementation: %v", err)
	}
	if len(implementation.Items) == 0 {
		t.Fatal("expected implementation results")
	}

	references, err := svc.FindReferences(context.Background(), ReferenceQuery{
		FilePath:   filePath,
		Line:       helperCallLine,
		SymbolName: "helper",
	})
	if err != nil {
		t.Fatalf("find references: %v", err)
	}
	if len(references.Items) == 0 {
		t.Fatal("expected reference results")
	}

	outline, err := svc.FileOutline(context.Background(), OutlineQuery{FilePath: filePath})
	if err != nil {
		t.Fatalf("file outline: %v", err)
	}
	if len(outline.Items) == 0 {
		t.Fatal("expected outline results")
	}

	rename, err := svc.Rename(context.Background(), RenameQuery{
		FilePath:   filePath,
		Line:       helperCallLine,
		SymbolName: "helper",
		NewName:    "renamed",
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if len(rename.Items) == 0 {
		t.Fatal("expected rename edits")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read renamed file: %v", err)
	}
	if !strings.Contains(string(data), "renamed()") {
		t.Fatalf("rename was not written to disk: %s", string(data))
	}
}

func containsAll(s string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func findLine(t *testing.T, content string, needle string) int {
	t.Helper()
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	t.Fatalf("line containing %q not found", needle)
	return 0
}
