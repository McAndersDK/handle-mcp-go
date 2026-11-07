package server

import (
	"context"
	"maps"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// tools is every tool by name. Each group sits in its own file.
func (s *Server) tools() map[string]toolFunc {
	all := map[string]toolFunc{}
	for _, group := range []map[string]toolFunc{s.readTools(), s.cartTools(), s.infoTools()} {
		maps.Copy(all, group)
	}
	return all
}

func (s *Server) readTools() map[string]toolFunc {
	return map[string]toolFunc{
		"search_products": func(ctx context.Context, a Args) (any, error) {
			limit := clamp(a.Int("limit"), 1, MaxResults)
			names, err := s.names(a.List("stores"))
			if err != nil {
				return nil, err
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (model.List[model.Product], error) {
				hits, err := compare.Search(ctx, st, a.Str("query"), limit, a.Str("sort"))
				return lst(hits), err
			}), nil
		},
		"search_recipes": func(ctx context.Context, a Args) (any, error) {
			limit := clamp(a.Int("limit"), 1, MaxResults)
			names, err := s.names(a.List("stores"))
			if err != nil {
				return nil, err
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (model.List[model.Recipe], error) {
				hits, err := st.Recipes(ctx, a.Str("query"), limit)
				return lst(hits), err
			}), nil
		},
		"get_recipe": func(ctx context.Context, a Args) (any, error) {
			return call(ctx, s, a.Str("store"), func(ctx context.Context, st store.Store) (model.RecipeDetails, error) {
				return st.Recipe(ctx, a.Str("recipe"), personsOf(a.OptInt("persons")))
			})
		},
		"list_recipes": func(ctx context.Context, a Args) (any, error) {
			limit := clamp(a.Int("limit"), 1, MaxResults)
			names, err := s.names(a.List("stores"))
			if err != nil {
				return nil, err
			}
			tags, category := a.List("tags"), a.OptStr("category")
			featured := len(tags) == 0 && (category == nil || *category == "")
			if tags == nil {
				tags = []string{}
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (model.List[model.Recipe], error) {
				if featured {
					hits, err := st.FeaturedRecipes(ctx, limit)
					return lst(hits), err
				}
				hits, err := st.RecipesByTag(ctx, tags, category, limit)
				return lst(hits), err
			}), nil
		},
		"list_offers": func(ctx context.Context, a Args) (any, error) {
			limit := clamp(a.Int("limit"), 1, MaxResults)
			names, err := s.names(a.List("stores"))
			if err != nil {
				return nil, err
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (model.List[model.Product], error) {
				hits, err := st.Offers(ctx, limit)
				return lst(hits), err
			}), nil
		},
	}
}
