package mcp

import (
	"context"
	"fmt"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/service"
	"github.com/bergo-tools/bergo-lsp-mcp/internal/types"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type definitionRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI    string `json:"rootUri" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type referenceRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI    string `json:"rootUri" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type outlineRequest struct {
	FilePath string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI  string `json:"rootUri" jsonschema_description:"Workspace root URI or path root URI or path;"`
}

type implementationRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI    string `json:"rootUri" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type renameRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI    string `json:"rootUri" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
	NewName    string `json:"newName" jsonschema:"required" jsonschema_description:"New symbol name"`
}

func New(svc *service.Service) *server.MCPServer {
	s := server.NewMCPServer(
		"bergo-lsp-mcp",
		"0.1.0",
		server.WithInstructions("Semantic code navigation and refactor actions via local LSP servers. Use for definitions, implementations, references, file symbols, and rename. `filePath` must be absolute. Definition/reference/implementation results are returned as `items[]`; references usually include the declaration; outline is a flat list with `containerName`; rename applies edits to disk and returns the updated changed lines. Invalid input or LSP/setup/root errors return tool errors. Successful LSP calls with no matches return empty `items`."),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	definitionTool := gomcp.NewTool(
		"find_definition",
		gomcp.WithDescription("Find semantic definitions for a symbol. Returns `items[]` and may contain multiple results. No matches => empty `items`; invalid input or LSP/root/setup problems => tool error."),
		gomcp.WithInputSchema[definitionRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	referencesTool := gomcp.NewTool(
		"find_references",
		gomcp.WithDescription("Find semantic references for a symbol across the workspace. Returns `items[]`; usually includes the declaration. No matches => empty `items`; invalid input or LSP/root/setup problems => tool error."),
		gomcp.WithInputSchema[referenceRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	implementationTool := gomcp.NewTool(
		"find_implementation",
		gomcp.WithDescription("Find semantic implementations for a symbol. Returns `items[]` and may contain multiple results. No matches => empty `items`; invalid input or LSP/root/setup problems => tool error."),
		gomcp.WithInputSchema[implementationRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	outlineTool := gomcp.NewTool(
		"file_outline",
		gomcp.WithDescription("List semantic symbols for one file. Returns a flat `items[]` outline; hierarchy is represented by `containerName`. No symbols => empty `items`; LSP/root/setup problems => tool error."),
		gomcp.WithInputSchema[outlineRequest](),
		gomcp.WithOutputSchema[types.OutlineResult](),
	)
	renameTool := gomcp.NewTool(
		"rename",
		gomcp.WithDescription("Apply a semantic rename for a symbol. Writes the LSP rename edits to disk and returns the updated changed lines in `items[]`. Invalid input, invalid new name, or LSP/root/setup problems => tool error."),
		gomcp.WithInputSchema[renameRequest](),
		gomcp.WithOutputSchema[types.RenameResult](),
	)

	s.AddTool(definitionTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args definitionRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.FindDefinition(ctx, service.DefinitionQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Found %d definition result(s)", len(result.Items))), nil
	}))
	s.AddTool(referencesTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args referenceRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.FindReferences(ctx, service.ReferenceQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Found %d reference result(s)", len(result.Items))), nil
	}))
	s.AddTool(implementationTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args implementationRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.FindImplementation(ctx, service.ImplementationQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Found %d implementation result(s)", len(result.Items))), nil
	}))
	s.AddTool(outlineTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args outlineRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.FileOutline(ctx, service.OutlineQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Found %d outline item(s)", len(result.Items))), nil
	}))
	s.AddTool(renameTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args renameRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.Rename(ctx, service.RenameQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Applied rename with %d changed line(s)", len(result.Items))), nil
	}))

	return s
}

func structuredResult(v any, fallback string) *gomcp.CallToolResult {
	return gomcp.NewToolResultStructured(v, fallback)
}
