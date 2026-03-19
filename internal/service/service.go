package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/config"
	"github.com/bergo-tools/bergo-lsp-mcp/internal/lsp"
	"github.com/bergo-tools/bergo-lsp-mcp/internal/types"

	"go.lsp.dev/protocol"
)

type Service struct {
	cfg     *config.Config
	manager *lsp.Manager
}

type DefinitionQuery struct {
	FilePath   string
	RootURI    string
	Line       int
	SymbolName string
	Index      int
}

type ReferenceQuery struct {
	FilePath   string
	RootURI    string
	Line       int
	SymbolName string
	Index      int
}

type OutlineQuery struct {
	FilePath string
	RootURI  string
}

func New(ctx context.Context, cfg *config.Config) (*Service, error) {
	_ = ctx
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	return &Service{
		cfg:     cfg,
		manager: lsp.NewManager(cfg),
	}, nil
}

func NewWithManager(cfg *config.Config, manager *lsp.Manager) *Service {
	return &Service{cfg: cfg, manager: manager}
}

func (s *Service) Close() error {
	if s.manager == nil {
		return nil
	}
	return s.manager.Close()
}

func (s *Service) FindReferences(ctx context.Context, query ReferenceQuery) (*types.QueryResult, error) {
	client, lang, absPath, warnings, lineZero, columnZero, err := s.prepareQuery(ctx, query.FilePath, query.RootURI, query.Line, query.SymbolName, query.Index)
	if err != nil {
		return nil, err
	}
	if err := client.EnsureSynced(ctx, absPath, lang.LanguageID); err != nil {
		return nil, err
	}

	locations, err := client.References(ctx, absPath, lineZero, columnZero)
	if err != nil {
		return nil, err
	}
	items, filterWarnings := filterLocationResults(query.SymbolName, locations)
	warnings = append(warnings, filterWarnings...)
	return &types.QueryResult{Items: items, Warnings: warnings}, nil
}

func (s *Service) FindDefinition(ctx context.Context, query DefinitionQuery) (*types.QueryResult, error) {
	client, lang, absPath, warnings, lineZero, columnZero, err := s.prepareQuery(ctx, query.FilePath, query.RootURI, query.Line, query.SymbolName, query.Index)
	if err != nil {
		return nil, err
	}
	if err := client.EnsureSynced(ctx, absPath, lang.LanguageID); err != nil {
		return nil, err
	}

	result, err := client.Definition(ctx, absPath, lineZero, columnZero)
	if err != nil {
		return nil, err
	}

	locations := append([]protocol.Location{}, result.Locations...)
	for _, link := range result.Links {
		locations = append(locations, protocol.Location{
			URI:   link.TargetURI,
			Range: link.TargetSelectionRange,
		})
	}
	items, filterWarnings := filterLocationResults(query.SymbolName, locations)
	warnings = append(warnings, filterWarnings...)
	return &types.QueryResult{Items: items, Warnings: warnings}, nil
}

func (s *Service) FileOutline(ctx context.Context, query OutlineQuery) (*types.OutlineResult, error) {
	absPath, err := filepath.Abs(query.FilePath)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute file path: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, fmt.Errorf("stat file %q: %w", absPath, err)
	}

	client, lang, err := s.manager.ClientForFile(ctx, absPath, query.RootURI)
	if err != nil {
		return nil, err
	}
	if err := client.EnsureSynced(ctx, absPath, lang.LanguageID); err != nil {
		return nil, err
	}

	symbols, err := client.DocumentSymbols(ctx, absPath)
	if err != nil {
		return nil, err
	}

	items := flattenSymbols(absPath, symbols)
	return &types.OutlineResult{Items: items}, nil
}

func (s *Service) prepareQuery(ctx context.Context, filePath string, rootURI string, line int, symbolName string, index int) (*lsp.Client, *config.LanguageLSP, string, []string, int, int, error) {
	_ = ctx
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, nil, "", nil, 0, 0, fmt.Errorf("resolve absolute file path: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, nil, "", nil, 0, 0, fmt.Errorf("stat file %q: %w", absPath, err)
	}
	if line <= 0 {
		return nil, nil, "", nil, 0, 0, errors.New("line must be >= 1")
	}

	client, lang, err := s.manager.ClientForFile(ctx, absPath, rootURI)
	if err != nil {
		return nil, nil, "", nil, 0, 0, err
	}

	lineZero, columnZero, warnings, err := symbolPosition(absPath, line, symbolName, index)
	if err != nil {
		return nil, nil, "", nil, 0, 0, err
	}

	return client, lang, absPath, warnings, lineZero, columnZero, nil
}

func symbolPosition(filePath string, line int, symbolName string, index int) (int, int, []string, error) {
	if strings.TrimSpace(symbolName) == "" {
		return 0, 0, nil, errors.New("symbolName is required")
	}
	if index < 0 {
		return 0, 0, nil, errors.New("index must be >= 0")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read file %q: %w", filePath, err)
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return 0, 0, nil, fmt.Errorf("line %d is out of range for %q", line, filePath)
	}

	lineText := lines[line-1]
	matches := findSymbolMatches(lineText, symbolName)
	if len(matches) > 0 {
		if index == 0 {
			if len(matches) == 1 {
				return line - 1, matches[0], nil, nil
			}
			return 0, 0, nil, fmt.Errorf(
				"symbolName %q appears %d times on line %d at columns %s; set index to 1-%d to disambiguate",
				symbolName,
				len(matches),
				line,
				formatColumns(matches),
				len(matches),
			)
		}
		if index > len(matches) {
			return 0, 0, nil, fmt.Errorf(
				"index %d is out of range for symbolName %q on line %d; available occurrences: %d at columns %s",
				index,
				symbolName,
				line,
				len(matches),
				formatColumns(matches),
			)
		}
		return line - 1, matches[index-1], nil, nil
	}

	fallback := strings.IndexFunc(lineText, func(r rune) bool { return r != ' ' && r != '\t' })
	if fallback < 0 {
		fallback = 0
	}
	return line - 1, fallback, []string{
		fmt.Sprintf("symbolName %q was not found on line %d; used fallback column %d", symbolName, line, fallback+1),
	}, nil
}

func findSymbolMatches(lineText string, symbolName string) []int {
	var matches []int
	offset := 0
	for {
		idx := strings.Index(lineText[offset:], symbolName)
		if idx < 0 {
			break
		}
		match := offset + idx
		matches = append(matches, match)
		offset = match + len(symbolName)
	}
	return matches
}

func formatColumns(matches []int) string {
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		columns = append(columns, fmt.Sprintf("%d", match+1))
	}
	return strings.Join(columns, ", ")
}

func filterLocationResults(symbolName string, locations []protocol.Location) ([]types.Position, []string) {
	positions := make([]types.Position, 0, len(locations))
	confirmed := make([]types.Position, 0, len(locations))
	unconfirmedCount := 0

	for _, location := range locations {
		position := toPosition(location.URI, location.Range)
		positions = append(positions, position)

		ok, known := lineContainsSymbol(position.FilePath, position.Line, symbolName)
		if !known {
			unconfirmedCount++
			continue
		}
		if ok {
			confirmed = append(confirmed, position)
		}
	}

	if len(confirmed) > 0 {
		var warnings []string
		if len(confirmed) != len(positions) {
			warnings = append(warnings, fmt.Sprintf("filtered %d result(s) using symbolName %q", len(positions)-len(confirmed), symbolName))
		}
		return confirmed, warnings
	}

	if len(positions) == 0 {
		return positions, nil
	}

	var warnings []string
	if unconfirmedCount > 0 {
		warnings = append(warnings, fmt.Sprintf("could not fully validate symbolName %q for %d result(s); returning raw LSP results", symbolName, unconfirmedCount))
	} else {
		warnings = append(warnings, fmt.Sprintf("symbolName %q did not match returned locations; returning raw LSP results", symbolName))
	}
	return positions, warnings
}

func lineContainsSymbol(filePath string, line int, symbolName string) (bool, bool) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, false
	}
	lines := strings.Split(string(data), "\n")
	if line <= 0 || line > len(lines) {
		return false, false
	}
	return strings.Contains(lines[line-1], symbolName), true
}

func toPosition(uri protocol.URI, rng protocol.Range) types.Position {
	return types.Position{
		FilePath:  lsp.URIToPath(uri),
		Line:      int(rng.Start.Line) + 1,
		Column:    int(rng.Start.Character) + 1,
		EndLine:   int(rng.End.Line) + 1,
		EndColumn: int(rng.End.Character) + 1,
	}
}

func flattenSymbols(filePath string, symbols []any) []types.OutlineItem {
	var items []types.OutlineItem
	for _, symbol := range symbols {
		switch v := symbol.(type) {
		case protocol.DocumentSymbol:
			items = append(items, flattenDocumentSymbol(filePath, v, "")...)
		case protocol.SymbolInformation:
			items = append(items, types.OutlineItem{
				Name:          v.Name,
				Kind:          v.Kind.String(),
				FilePath:      lsp.URIToPath(v.Location.URI),
				Line:          int(v.Location.Range.Start.Line) + 1,
				Column:        int(v.Location.Range.Start.Character) + 1,
				EndLine:       int(v.Location.Range.End.Line) + 1,
				EndColumn:     int(v.Location.Range.End.Character) + 1,
				ContainerName: v.ContainerName,
			})
		}
	}
	return items
}

func flattenDocumentSymbol(filePath string, symbol protocol.DocumentSymbol, container string) []types.OutlineItem {
	items := []types.OutlineItem{{
		Name:          symbol.Name,
		Kind:          symbol.Kind.String(),
		Detail:        symbol.Detail,
		FilePath:      filePath,
		Line:          int(symbol.SelectionRange.Start.Line) + 1,
		Column:        int(symbol.SelectionRange.Start.Character) + 1,
		EndLine:       int(symbol.SelectionRange.End.Line) + 1,
		EndColumn:     int(symbol.SelectionRange.End.Character) + 1,
		ContainerName: container,
	}}
	for _, child := range symbol.Children {
		items = append(items, flattenDocumentSymbol(filePath, child, symbol.Name)...)
	}
	return items
}
