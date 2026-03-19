package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesEmbeddedDefaultsWhenConfigMissing(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	lang, err := cfg.MatchLanguage("main.go")
	if err != nil {
		t.Fatalf("match default go language: %v", err)
	}
	if lang.Command != "gopls" {
		t.Fatalf("unexpected default command: %s", lang.Command)
	}
}

func TestLoadMergesUserConfigWithDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	configData := `{
		"rootMarkers": [".custom-root"],
		"languages": [
			{
				"name": "go",
				"extensions": [".go"],
				"languageId": "go",
				"command": "custom-gopls",
				"args": ["serve"],
				"rootDirStrategy": "auto"
			},
			{
				"name": "markdown",
				"extensions": [".md"],
				"languageId": "markdown",
				"command": "marksman",
				"args": ["server"],
				"rootDirStrategy": "auto"
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}

	goLang, err := cfg.MatchLanguage("main.go")
	if err != nil {
		t.Fatalf("match overridden go language: %v", err)
	}
	if goLang.Command != "custom-gopls" {
		t.Fatalf("unexpected overridden command: %s", goLang.Command)
	}

	mdLang, err := cfg.MatchLanguage("README.md")
	if err != nil {
		t.Fatalf("match custom markdown language: %v", err)
	}
	if mdLang.Command != "marksman" {
		t.Fatalf("unexpected markdown command: %s", mdLang.Command)
	}

	foundMarker := false
	for _, marker := range cfg.RootMarkers {
		if marker == ".custom-root" {
			foundMarker = true
			break
		}
	}
	if !foundMarker {
		t.Fatal("expected merged root marker")
	}
}

func TestMatchLanguage(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Languages: []LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "gopls"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	lang, err := cfg.MatchLanguage("main.go")
	if err != nil {
		t.Fatalf("match language: %v", err)
	}
	if lang.Name != "go" {
		t.Fatalf("unexpected language: %s", lang.Name)
	}
}

func TestMatchLanguageMissing(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Languages: []LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "gopls"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if _, err := cfg.MatchLanguage("main.ts"); err == nil {
		t.Fatal("expected match error")
	}
}

func TestResolveRootAuto(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "repo")
	srcDir := filepath.Join(root, "nested", "pkg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	cfg := &Config{
		Languages: []LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "gopls"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	got, err := cfg.ResolveRoot(filepath.Join(srcDir, "main.go"), &cfg.Languages[0], "")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	if got != root {
		t.Fatalf("unexpected root: got %s want %s", got, root)
	}
}

func TestResolveRootUsesExplicitRootURI(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Languages: []LanguageLSP{
			{Name: "go", Extensions: []string{".go"}, LanguageID: "go", Command: "gopls"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	tmpDir := t.TempDir()
	got, err := cfg.ResolveRoot(filepath.Join(tmpDir, "main.go"), &cfg.Languages[0], "file://"+filepath.ToSlash(tmpDir))
	if err != nil {
		t.Fatalf("resolve explicit root: %v", err)
	}
	if got != tmpDir {
		t.Fatalf("unexpected explicit root: got %s want %s", got, tmpDir)
	}
}
