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
	RootURI    string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type referenceRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI    string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type outlineRequest struct {
	FilePath string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute file path; relative paths are not supported"`
	RootURI  string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path root URI or path;"`
}

func New(svc *service.Service) *server.MCPServer {
	s := server.NewMCPServer(
		"bergo-lsp-mcp",
		"0.1.0",
		server.WithInstructions("Semantic code navigation for local source files via language servers (LSP). Use this server when you need symbol definitions, symbol references, or a file symbol outline. This server is not for plain text search or arbitrary file reading. It works best when filePath is an absolute path inside a local project and the matching language server is installed."),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	definitionTool := gomcp.NewTool(
		"find_definition",
		gomcp.WithDescription("Use this to jump to the semantic definition of a symbol in source code through a configured LSP server. Best for 'where is this symbol defined?' queries."),
		gomcp.WithInputSchema[definitionRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	referencesTool := gomcp.NewTool(
		"find_references",
		gomcp.WithDescription("Use this to find semantic references/usages of a symbol across the workspace through a configured LSP server. Best for 'where is this symbol used?' queries."),
		gomcp.WithInputSchema[referenceRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	outlineTool := gomcp.NewTool(
		"file_outline",
		gomcp.WithDescription("Use this to list the semantic symbols and structure of a file through a configured LSP server. Best for understanding a file's top-level and nested declarations without reading the whole file."),
		gomcp.WithInputSchema[outlineRequest](),
		gomcp.WithOutputSchema[types.OutlineResult](),
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
	s.AddTool(outlineTool, gomcp.NewTypedToolHandler(func(ctx context.Context, _ gomcp.CallToolRequest, args outlineRequest) (*gomcp.CallToolResult, error) {
		result, err := svc.FileOutline(ctx, service.OutlineQuery(args))
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return structuredResult(result, fmt.Sprintf("Found %d outline item(s)", len(result.Items))), nil
	}))

	return s
}

func structuredResult(v any, fallback string) *gomcp.CallToolResult {
	return gomcp.NewToolResultStructured(v, fallback)
}
