package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lspuri "go.lsp.dev/uri"
)

const (
	RootStrategyAuto    = "auto"
	RootStrategyFileDir = "file_dir"
)

type Config struct {
	RootMarkers []string      `json:"rootMarkers,omitempty"`
	Languages   []LanguageLSP `json:"languages"`
}

type LanguageLSP struct {
	Name                  string            `json:"name"`
	Extensions            []string          `json:"extensions"`
	LanguageID            string            `json:"languageId"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	RootDirStrategy       string            `json:"rootDirStrategy,omitempty"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
	RootMarkers           []string          `json:"rootMarkers,omitempty"`
}

//go:embed defaults.json
var embeddedDefaults []byte

func Load(path string) (*Config, error) {
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var userCfg Config
	if err := json.Unmarshal(data, &userCfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	merged, err := merge(cfg, &userCfg)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func loadDefaults() (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(embeddedDefaults, &cfg); err != nil {
		return nil, fmt.Errorf("parse embedded defaults: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate embedded defaults: %w", err)
	}
	return &cfg, nil
}

func merge(base *Config, override *Config) (*Config, error) {
	if base == nil {
		base = &Config{}
	}
	if override == nil {
		override = &Config{}
	}

	merged := &Config{
		RootMarkers: append([]string{}, base.RootMarkers...),
		Languages:   append([]LanguageLSP{}, base.Languages...),
	}

	if len(override.RootMarkers) > 0 {
		merged.RootMarkers = append(merged.RootMarkers, override.RootMarkers...)
	}

	indexByName := make(map[string]int, len(merged.Languages))
	for i, lang := range merged.Languages {
		indexByName[lang.Name] = i
	}
	for _, lang := range override.Languages {
		if idx, ok := indexByName[lang.Name]; ok {
			merged.Languages[idx] = lang
			continue
		}
		merged.Languages = append(merged.Languages, lang)
		indexByName[lang.Name] = len(merged.Languages) - 1
	}

	if err := merged.Validate(); err != nil {
		return nil, err
	}
	return merged, nil
}

func (c *Config) Validate() error {
	if len(c.Languages) == 0 {
		return errors.New("config.languages is required")
	}

	seenExtensions := map[string]string{}
	for i := range c.Languages {
		lang := &c.Languages[i]
		if strings.TrimSpace(lang.Name) == "" {
			return fmt.Errorf("languages[%d].name is required", i)
		}
		if strings.TrimSpace(lang.LanguageID) == "" {
			return fmt.Errorf("languages[%d].languageId is required", i)
		}
		if strings.TrimSpace(lang.Command) == "" {
			return fmt.Errorf("languages[%d].command is required", i)
		}
		if len(lang.Extensions) == 0 {
			return fmt.Errorf("languages[%d].extensions is required", i)
		}
		if lang.RootDirStrategy == "" {
			lang.RootDirStrategy = RootStrategyAuto
		}
		if lang.RootDirStrategy != RootStrategyAuto && lang.RootDirStrategy != RootStrategyFileDir {
			return fmt.Errorf("languages[%d].rootDirStrategy must be %q or %q", i, RootStrategyAuto, RootStrategyFileDir)
		}
		for j, ext := range lang.Extensions {
			normalized := normalizeExtension(ext)
			if normalized == "" {
				return fmt.Errorf("languages[%d].extensions[%d] is empty", i, j)
			}
			if prev, exists := seenExtensions[normalized]; exists {
				return fmt.Errorf("extension %q is declared by both %q and %q", normalized, prev, lang.Name)
			}
			lang.Extensions[j] = normalized
			seenExtensions[normalized] = lang.Name
		}
	}

	c.RootMarkers = normalizeMarkers(c.RootMarkers)
	return nil
}

func (c *Config) MatchLanguage(path string) (*LanguageLSP, error) {
	ext := strings.ToLower(filepath.Ext(path))
	for i := range c.Languages {
		lang := &c.Languages[i]
		for _, candidate := range lang.Extensions {
			if candidate == ext {
				return lang, nil
			}
		}
	}
	return nil, fmt.Errorf("no LSP configured for extension %q", ext)
}

func (c *Config) ResolveRoot(filePath string, lang *LanguageLSP, rootURI string) (string, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve absolute file path: %w", err)
	}

	if strings.TrimSpace(rootURI) != "" {
		return normalizeRootURI(rootURI)
	}

	dir := filepath.Dir(absPath)
	if lang.RootDirStrategy == RootStrategyFileDir {
		return dir, nil
	}

	markers := normalizeMarkers(lang.RootMarkers)
	if len(markers) == 0 {
		markers = c.RootMarkers
	}
	if len(markers) == 0 {
		markers = []string{".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml"}
	}

	root, found := findRoot(dir, markers)
	if found {
		return root, nil
	}
	return dir, nil
}

func normalizeRootURI(rootURI string) (string, error) {
	trimmed := strings.TrimSpace(rootURI)
	if trimmed == "" {
		return "", errors.New("rootUri is empty")
	}
	if strings.HasPrefix(trimmed, "file://") {
		parsed, err := lspuri.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse rootUri %q: %w", trimmed, err)
		}
		return filepath.Abs(parsed.Filename())
	}
	rootPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve rootUri path %q: %w", trimmed, err)
	}
	return rootPath, nil
}

func findRoot(start string, markers []string) (string, bool) {
	current := start
	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current, true
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func normalizeExtension(ext string) string {
	trimmed := strings.TrimSpace(strings.ToLower(ext))
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, ".") {
		return "." + trimmed
	}
	return trimmed
}

func normalizeMarkers(markers []string) []string {
	out := make([]string, 0, len(markers))
	seen := map[string]struct{}{}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		if _, exists := seen[marker]; exists {
			continue
		}
		seen[marker] = struct{}{}
		out = append(out, marker)
	}
	sort.Strings(out)
	return out
}
