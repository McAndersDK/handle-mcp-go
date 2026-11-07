// Command handle-mcp is the MCP server for Danish grocery stores, and the login command.
//
//	handle-mcp                           serve MCP over stdio (what an MCP client starts)
//	handle-mcp login <store> [--from-env]  save a store login (see package cli)
//	handle-mcp logout <store>            remove it
//
// stdout is the JSON-RPC stream: nothing here writes to it. Logs go to stderr through log/slog.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // Danish delivery times must work on a machine with no zone database

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/cli"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/server"
)

// version is set at release time: -ldflags "-X main.version=1.0.0".
var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	// INFO to stderr. Nothing logs a URL: a URL can hold ids and tokens, and clients keep stderr in log files.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := server.Registry()
	if len(args) > 0 { // a human in a terminal, not an MCP client
		stores := make(map[string]cli.Factory, len(registry))
		for name, f := range registry {
			stores[name] = cli.Factory(f)
		}
		return cli.Run(ctx, args, stores, cli.DefaultTerminal())
	}
	if err := server.New(registry, version).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		slog.Error("server stopped", "type", fmt.Sprintf("%T", err))
		return 1
	}
	return 0
}
