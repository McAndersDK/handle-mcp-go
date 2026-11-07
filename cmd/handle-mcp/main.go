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
	"runtime/debug"
	"strings"
	"syscall"
	_ "time/tzdata" // Danish delivery times must work on a machine with no zone database

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/internal/cli"
	"github.com/McAndersDK/handle-mcp-go/internal/server"
)

// version is set at release time: -ldflags "-X main.version=1.0.0".
var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

// buildVersion returns the release version. A `go install ...@v1.0.0` binary has no ldflags, so it
// takes the module version from the build info instead.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(version, info, ok)
}

// resolveVersion prefers the ldflags version. If that is still "dev", it uses the module version
// without its leading "v", or "dev" when the build info has none (a local build says "(devel)").
func resolveVersion(ldflags string, info *debug.BuildInfo, ok bool) string {
	if ldflags != "dev" {
		return ldflags
	}
	if !ok || info == nil || info.Main.Version == "(devel)" {
		return "dev"
	}
	if v := strings.TrimPrefix(info.Main.Version, "v"); v != "" {
		return v
	}
	return "dev"
}

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
	if err := server.New(registry, buildVersion()).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		slog.Error("server stopped", "type", fmt.Sprintf("%T", err))
		return 1
	}
	return 0
}
