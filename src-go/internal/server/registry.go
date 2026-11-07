//go:build !e2e

package server

import (
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store/bilkatogo"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store/nemlig"
)

// Registry is every store the server and `handle-mcp login` know. It lives here and not in
// package store, which the stores import. A new store is one line.
func Registry() map[string]Factory {
	return map[string]Factory{
		"nemlig":    func() store.Store { return nemlig.New(nil) },
		"bilkatogo": func() store.Store { return bilkatogo.New(nil) },
	}
}
