//go:build e2e

package server

import (
	"context"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// This registry exists only in a binary built with `-tags e2e`, for the end-to-end test in
// cmd/handle-mcp. It has no real store, so the test makes no store call and reads no login.
// "refuses" answers with a StoreError, "crashes" panics: both must come out as JSON, never as
// anything else on stdout.

type e2eStore struct {
	store.Store // every method not written below panics, which the server recovers
	name        string
	crash       bool
}

func (s e2eStore) Name() string { return s.name }

func (s e2eStore) Search(context.Context, string, int) ([]model.Product, error) {
	if s.crash {
		panic("search on a store that crashes")
	}
	return nil, store.Errorf("API'et svarer 500")
}

// Registry is two stores that fail in different ways.
func Registry() map[string]Factory {
	return map[string]Factory{
		"refuses": func() store.Store { return e2eStore{name: "refuses"} },
		"crashes": func() store.Store { return e2eStore{name: "crashes", crash: true} },
	}
}
