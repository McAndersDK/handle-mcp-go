// Package server is the MCP server: every tool, store selection and error handling. A port of
// server.py (ADR 0005).
//
// stdout is the JSON-RPC stream: nothing here writes to it, and logging goes to stderr through
// log/slog. Every answer and every error leaves through out.go, the one exit.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/privacy"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// Limits of the tools.
const (
	MaxItems    = 50 // per set_cart_quantities or compare_basket call
	MaxQuantity = 50 // per product
	MaxResults  = 25
	MaxDays     = 14
	MinPersons  = 2 // nemlig: 2-12
	MaxPersons  = 12
)

// instructions tell the client what the server is for.
const instructions = "Tools for Danish grocery stores with home delivery. You can search, compare " +
	"baskets, find recipes, list delivery times and change cart quantities. You can never order or pay: the human " +
	"always does that on the store's website."

// Factory makes a store. A new store is one line in Registry.
type Factory = func() store.Store

// Server holds the stores. A store is made on first use and kept, so it keeps its login.
type Server struct {
	factories map[string]Factory
	version   string
	defs      map[string]toolDef

	mu        sync.Mutex
	instances map[string]store.Store
}

// New makes a server over the given stores.
func New(factories map[string]Factory, version string) *Server {
	return &Server{factories: factories, version: version, defs: loadTools(), instances: map[string]store.Store{}}
}

// toolFunc is the work of one tool. It returns the answer, or an error: a *toolError is shown as
// it is, anything else becomes "uventet fejl".
type toolFunc func(ctx context.Context, a Args) (any, error)

// MCP builds the MCP server with every tool registered through the low-level AddTool.
func (s *Server) MCP() *mcp.Server {
	m := mcp.NewServer(&mcp.Implementation{Name: "handle", Version: s.version}, &mcp.ServerOptions{
		Instructions: instructions,
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}, // tools only: no logging, prompts or resources
	})
	tools := s.tools()
	for name, fn := range tools {
		def, ok := s.defs[name]
		if !ok {
			panic(fmt.Errorf("tool %s is not in tools.json", name))
		}
		m.AddTool(def.tool(), s.wrap(def, fn))
	}
	if len(s.defs) != len(tools) {
		panic(fmt.Errorf("tools.json has %d tools, the server %d", len(s.defs), len(tools)))
	}
	return m
}

// Run serves one session over the transport (stdio, for the binary).
func (s *Server) Run(ctx context.Context, t mcp.Transport) error { return s.MCP().Run(ctx, t) }

// wrap decodes the arguments, runs the tool and leaves through the exit. It never returns a Go
// error: the model gets an isError result, so it can read what went wrong.
func (s *Server) wrap(def toolDef, fn toolFunc) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, _ error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("tool panicked", "tool", def.Name, "type", fmt.Sprintf("%T", r))
				res = failure(fmt.Sprintf("Uventet fejl i %s (%T). Se serverens log.", def.Name, r))
			}
		}()
		args, err := decodeArgs(def.params, req.Params.Arguments)
		if err == nil {
			var value any
			if value, err = fn(ctx, args); err == nil {
				return answer(value), nil
			}
		}
		if te, ok := err.(*toolError); ok {
			return failure(te.msg), nil
		}
		slog.Error("tool failed", "tool", def.Name, "type", fmt.Sprintf("%T", err))
		return failure(fmt.Sprintf("Uventet fejl i %s (%T). Se serverens log.", def.Name, err)), nil
	}
}

// store returns the named store, made on first use.
func (s *Server) store(name string) (store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.instances[name]; ok {
		return st, nil
	}
	f, ok := s.factories[name]
	if !ok {
		return nil, s.unknownStore(name)
	}
	st := f()
	s.instances[name] = st
	return st, nil
}

// names are the stores a call is about: the requested ones, or all. An unknown name fails early.
// A name given twice counts once.
func (s *Server) names(requested []string) ([]string, error) {
	names := requested
	if len(names) == 0 {
		names = s.storeNames()
	}
	if len(names) == 0 {
		return nil, toolErrorf("Ingen butikker er sat op endnu.")
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if _, err := s.store(n); err != nil {
			return nil, err
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// errorAnswer is a failing store inside an answer that has other stores in it.
type errorAnswer struct {
	Error string `json:"error"`
}

func errorEntry(name string, err error) errorAnswer {
	text, _ := privacy.ScrubText(storeReason(name, err)).(string)
	return errorAnswer{text}
}

// call runs one method of one store. A failure is a tool error with the store's name in front.
func call[T any](ctx context.Context, s *Server, name string, work func(context.Context, store.Store) (T, error)) (T, error) {
	var zero T
	st, err := s.store(name)
	if err != nil {
		return zero, err
	}
	res := compare.Gather(ctx, []compare.Target{{Name: name, Store: st}}, work)[0] // recovers a panic
	if res.Err != nil {
		return zero, &toolError{errorEntry(name, res.Err).Error}
	}
	return res.Value, nil
}

// gather runs work for every store at once. One store failing never stops the others.
func gather[T any](ctx context.Context, s *Server, names []string, work func(context.Context, store.Store) (T, error)) []compare.Result[T] {
	targets := make([]compare.Target, len(names))
	for i, n := range names {
		st, _ := s.store(n) // names() checked it
		targets[i] = compare.Target{Name: n, Store: st}
	}
	return compare.Gather(ctx, targets, work)
}

// each is gather as an answer: the value of each store by name, or {"error": ...} for a failing one.
func each[T any](ctx context.Context, s *Server, names []string, work func(context.Context, store.Store) (T, error)) map[string]any {
	return entries(gather(ctx, s, names, work))
}

func entries[T any](results []compare.Result[T]) map[string]any {
	out := make(map[string]any, len(results))
	for _, r := range results {
		if r.Err != nil {
			out[r.Name] = errorEntry(r.Name, r.Err)
		} else {
			out[r.Name] = r.Value
		}
	}
	return out
}
