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
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute or relative file path"`
	RootURI    string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type referenceRequest struct {
	FilePath   string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute or relative file path"`
	RootURI    string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path;"`
	Line       int    `json:"line" jsonschema:"required,minimum=1" jsonschema_description:"1-based line number"`
	SymbolName string `json:"symbolName" jsonschema:"required" jsonschema_description:"Symbol name on the target line"`
	Index      int    `json:"index,omitempty" jsonschema:"minimum=0" jsonschema_description:"Optional 1-based occurrence index of symbolName on the line; 0 means unspecified"`
}

type outlineRequest struct {
	FilePath string `json:"filePath" jsonschema:"required" jsonschema_description:"Absolute or relative file path"`
	RootURI  string `json:"rootUri,omitempty" jsonschema_description:"Workspace root URI or path root URI or path;"`
}

func New(svc *service.Service) *server.MCPServer {
	s := server.NewMCPServer(
		"bergo-lsp-mcp",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	definitionTool := gomcp.NewTool(
		"find_definition",
		gomcp.WithDescription("Find symbol definitions through a configured LSP server."),
		gomcp.WithInputSchema[definitionRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	referencesTool := gomcp.NewTool(
		"find_references",
		gomcp.WithDescription("Find symbol references through a configured LSP server."),
		gomcp.WithInputSchema[referenceRequest](),
		gomcp.WithOutputSchema[types.QueryResult](),
	)
	outlineTool := gomcp.NewTool(
		"file_outline",
		gomcp.WithDescription("List the symbols and structural outline of a file through a configured LSP server."),
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
