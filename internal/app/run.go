package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/config"
	mcpserver "github.com/bergo-tools/bergo-lsp-mcp/internal/mcp"
	"github.com/bergo-tools/bergo-lsp-mcp/internal/service"

	"github.com/mark3labs/mcp-go/server"
)

func Run(ctx context.Context) int {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	svc, err := service.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create service: %v\n", err)
		return 1
	}
	defer func() {
		if closeErr := svc.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "close service: %v\n", closeErr)
		}
	}()

	s := mcpserver.New(svc)
	if err := server.ServeStdio(s); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "serve stdio: %v\n", err)
		return 1
	}

	return 0
}

func resolveConfigPath() string {
	if path := os.Getenv("BERGO_LSP_MCP_CONFIG"); path != "" {
		return path
	}
	return "config.json"
}
