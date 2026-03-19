package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/bergo-tools/bergo-lsp-mcp/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(app.Run(ctx))
}
