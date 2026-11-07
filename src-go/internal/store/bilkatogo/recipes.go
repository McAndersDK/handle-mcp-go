package bilkatogo

import (
	"context"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// BilkaToGo has no recipe API that we know of: the tools say so.

// Recipes is not implemented.
func (s *Store) Recipes(context.Context, string, int) ([]model.Recipe, error) {
	return nil, store.ErrNotImplemented
}

// Recipe is not implemented.
func (s *Store) Recipe(context.Context, string, *int) (model.RecipeDetails, error) {
	return model.RecipeDetails{}, store.ErrNotImplemented
}

// FeaturedRecipes is not implemented.
func (s *Store) FeaturedRecipes(context.Context, int) ([]model.Recipe, error) {
	return nil, store.ErrNotImplemented
}

// RecipesByTag is not implemented.
func (s *Store) RecipesByTag(context.Context, []string, *string, int) ([]model.Recipe, error) {
	return nil, store.ErrNotImplemented
}
