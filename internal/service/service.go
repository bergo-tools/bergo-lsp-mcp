package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

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

type ImplementationQuery struct {
	FilePath   string
	RootURI    string
	Line       int
	SymbolName string
	Index      int
}

type RenameQuery struct {
	FilePath   string
	RootURI    string
	Line       int
	SymbolName string
	Index      int
	NewName    string
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

func (s *Service) FindImplementation(ctx context.Context, query ImplementationQuery) (*types.QueryResult, error) {
	client, lang, absPath, warnings, lineZero, columnZero, err := s.prepareQuery(ctx, query.FilePath, query.RootURI, query.Line, query.SymbolName, query.Index)
	if err != nil {
		return nil, err
	}
	if err := client.EnsureSynced(ctx, absPath, lang.LanguageID); err != nil {
		return nil, err
	}

	result, err := client.Implementation(ctx, absPath, lineZero, columnZero)
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

func (s *Service) Rename(ctx context.Context, query RenameQuery) (*types.RenameResult, error) {
	if strings.TrimSpace(query.NewName) == "" {
		return nil, errors.New("newName is required")
	}

	client, lang, absPath, warnings, lineZero, columnZero, err := s.prepareQuery(ctx, query.FilePath, query.RootURI, query.Line, query.SymbolName, query.Index)
	if err != nil {
		return nil, err
	}
	if err := client.EnsureSynced(ctx, absPath, lang.LanguageID); err != nil {
		return nil, err
	}

	edit, err := client.Rename(ctx, absPath, lineZero, columnZero, query.NewName)
	if err != nil {
		return nil, err
	}
	items, err := applyWorkspaceEdit(edit)
	if err != nil {
		return nil, err
	}
	return &types.RenameResult{Items: items, Warnings: warnings}, nil
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
	return &types.OutlineResult{FilePath: absPath, Items: items}, nil
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

func filterLocationResults(symbolName string, locations []protocol.Location) ([]string, []string) {
	positions := make([]string, 0, len(locations))
	confirmed := make([]string, 0, len(locations))
	unconfirmedCount := 0

	for _, location := range locations {
		position := formatLocation(location.URI, location.Range)
		positions = append(positions, position)

		filePath := lsp.URIToPath(location.URI)
		line := int(location.Range.Start.Line) + 1
		ok, known := lineContainsSymbol(filePath, line, symbolName)
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

func flattenSymbols(filePath string, symbols []any) []string {
	var items []string
	for _, symbol := range symbols {
		switch v := symbol.(type) {
		case protocol.DocumentSymbol:
			items = append(items, flattenDocumentSymbol(filePath, v, "")...)
		case protocol.SymbolInformation:
			items = append(items, formatOutlineItem(
				v.Kind.String(),
				v.Name,
				"",
				v.ContainerName,
				int(v.Location.Range.Start.Line)+1,
				int(v.Location.Range.End.Line)+1,
			))
		}
	}
	return items
}

func flattenDocumentSymbol(filePath string, symbol protocol.DocumentSymbol, container string) []string {
	_ = filePath
	items := []string{formatOutlineItem(
		symbol.Kind.String(),
		symbol.Name,
		symbol.Detail,
		container,
		int(symbol.SelectionRange.Start.Line)+1,
		int(symbol.SelectionRange.End.Line)+1,
	)}
	for _, child := range symbol.Children {
		items = append(items, flattenDocumentSymbol(filePath, child, symbol.Name)...)
	}
	return items
}

type fileEdit struct {
	start protocol.Position
	end   protocol.Position
	text  string
}

func applyWorkspaceEdit(edit *protocol.WorkspaceEdit) ([]string, error) {
	if edit == nil {
		return nil, nil
	}

	editsByFile := make(map[string][]fileEdit)
	for uri, edits := range edit.Changes {
		filePath := lsp.URIToPath(protocol.URI(uri))
		for _, edit := range edits {
			editsByFile[filePath] = append(editsByFile[filePath], fileEdit{
				start: edit.Range.Start,
				end:   edit.Range.End,
				text:  edit.NewText,
			})
		}
	}
	for _, change := range edit.DocumentChanges {
		filePath := lsp.URIToPath(protocol.URI(change.TextDocument.URI))
		for _, edit := range change.Edits {
			editsByFile[filePath] = append(editsByFile[filePath], fileEdit{
				start: edit.Range.Start,
				end:   edit.Range.End,
				text:  edit.NewText,
			})
		}
	}

	var filePaths []string
	for filePath := range editsByFile {
		filePaths = append(filePaths, filePath)
	}
	sort.Strings(filePaths)

	var items []string
	for _, filePath := range filePaths {
		fileItems, err := applyFileEdits(filePath, editsByFile[filePath])
		if err != nil {
			return nil, err
		}
		items = append(items, fileItems...)
	}
	return items, nil
}

func formatLocation(uri protocol.URI, rng protocol.Range) string {
	filePath := lsp.URIToPath(uri)
	line := int(rng.Start.Line) + 1
	return fmt.Sprintf("%s:%d: %s", filePath, line, lineText(filePath, line))
}

func formatOutlineItem(kind string, name string, detail string, container string, line int, endLine int) string {
	label := name
	if container != "" {
		label = container + "." + name
	}
	signature := formatOutlineSignature(label, detail)
	if signature == "" {
		signature = label
	}
	if endLine <= line {
		return fmt.Sprintf("%s %s [line %d]", strings.ToLower(kind), signature, line)
	}
	return fmt.Sprintf("%s %s [line %d-%d]", strings.ToLower(kind), signature, line, endLine)
}

func applyFileEdits(filePath string, edits []fileEdit) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", filePath, err)
	}

	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start.Line != edits[j].start.Line {
			return edits[i].start.Line > edits[j].start.Line
		}
		return edits[i].start.Character > edits[j].start.Character
	})

	changedLines := make(map[int]struct{})
	content := string(data)
	for _, edit := range edits {
		start, err := positionToOffset(content, edit.start)
		if err != nil {
			return nil, fmt.Errorf("map rename start position for %q: %w", filePath, err)
		}
		end, err := positionToOffset(content, edit.end)
		if err != nil {
			return nil, fmt.Errorf("map rename end position for %q: %w", filePath, err)
		}
		if start > end || start < 0 || end > len(content) {
			return nil, fmt.Errorf("invalid rename range for %q", filePath)
		}
		content = content[:start] + edit.text + content[end:]
		for line := int(edit.start.Line) + 1; line <= int(edit.end.Line)+1; line++ {
			changedLines[line] = struct{}{}
		}
		if len(edit.text) > 0 {
			for _, line := range changedLineNumbers(edit.start, edit.text) {
				changedLines[line] = struct{}{}
			}
		}
	}

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write file %q: %w", filePath, err)
	}

	lines := strings.Split(content, "\n")
	var lineNumbers []int
	for line := range changedLines {
		if line > 0 && line <= len(lines) {
			lineNumbers = append(lineNumbers, line)
		}
	}
	sort.Ints(lineNumbers)

	items := make([]string, 0, len(lineNumbers))
	for _, line := range lineNumbers {
		items = append(items, fmt.Sprintf("%s:%d: %s", filePath, line, strings.TrimSpace(lines[line-1])))
	}
	return items, nil
}

func positionToOffset(content string, pos protocol.Position) (int, error) {
	line := int(pos.Line)
	character := int(pos.Character)
	if line < 0 || character < 0 {
		return 0, errors.New("negative position")
	}

	offset := 0
	currentLine := 0
	for currentLine < line {
		if offset >= len(content) {
			return 0, errors.New("line out of range")
		}
		idx := strings.IndexByte(content[offset:], '\n')
		if idx < 0 {
			return 0, errors.New("line out of range")
		}
		offset += idx + 1
		currentLine++
	}

	lineContent := content[offset:]
	if idx := strings.IndexByte(lineContent, '\n'); idx >= 0 {
		lineContent = lineContent[:idx]
	}

	byteInLine, err := utf16ColumnToByteOffset(lineContent, character)
	if err != nil {
		return 0, err
	}
	return offset + byteInLine, nil
}

func utf16ColumnToByteOffset(line string, column int) (int, error) {
	if column == 0 {
		return 0, nil
	}

	byteOffset := 0
	utf16Units := 0
	for _, r := range line {
		if utf16Units >= column {
			return byteOffset, nil
		}
		units := len(utf16.Encode([]rune{r}))
		if utf16Units+units > column {
			return 0, errors.New("column splits a rune")
		}
		utf16Units += units
		byteOffset += len(string(r))
	}
	if utf16Units == column {
		return byteOffset, nil
	}
	return 0, errors.New("column out of range")
}

func changedLineNumbers(start protocol.Position, newText string) []int {
	lines := strings.Count(newText, "\n")
	out := make([]int, 0, lines+1)
	for i := 0; i <= lines; i++ {
		out = append(out, int(start.Line)+1+i)
	}
	return out
}

func formatOutlineSignature(label string, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return label
	}
	if strings.HasPrefix(detail, "(") {
		return label + detail
	}
	return label + " " + detail
}

func lineText(filePath string, line int) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line <= 0 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}
