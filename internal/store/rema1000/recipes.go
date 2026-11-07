package rema1000

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// REMA's recipes come from the public API behind rema1000.dk/opskrifter (the same JSON the page
// is built from). It needs no login. Prices come from the recipe's own product lines.
const (
	entitiesPath      = "rema1000dk/entities"
	maxRecipePages    = 5
	recipePageSize    = 100
	recipeNotFoundMsg = "REMA 1000 har ingen opskrift med id %q. Brug et id fra list_recipes."
)

type apiTag struct {
	ID     string `json:"id"`
	Fields struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	} `json:"fields"`
}

// Recipes searches REMA's recipes.
func (s *Store) Recipes(ctx context.Context, query string, limit int) ([]model.Recipe, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.recipeList(ctx, "search/recipes", url.Values{"query": {query}}, limit, nil)
}

// FeaturedRecipes are the most popular recipes, the ones the webshop shows first.
func (s *Store) FeaturedRecipes(ctx context.Context, limit int) ([]model.Recipe, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.recipeList(ctx, entitiesPath, listing("-popularity"), limit, nil)
}

// RecipesByTag lists recipes that have every tag. `category` is one more tag (aftensmad, frokost,
// ...): REMA has tags and no categories. An unknown tag answers with the list of known ones.
func (s *Store) RecipesByTag(ctx context.Context, tags []string, category *string, limit int) ([]model.Recipe, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	names := append([]string(nil), tags...)
	if category != nil {
		names = append(names, *category)
	}
	if len(names) == 0 {
		return s.recipeList(ctx, entitiesPath, listing("-popularity"), limit, nil)
	}
	known, err := pages[apiTag](ctx, s, call{method: "GET", path: entitiesPath,
		query: url.Values{"filter[type]": {"recipeTag"}}}, "opskrifternes tags", recipePageSize, 2, nil)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, n := range names {
		id, ok := tagID(known, n)
		if !ok {
			return nil, store.Errorf("REMA 1000 kender ikke tagget %q. Kendte tags: %s.", n, tagTitles(known))
		}
		ids = append(ids, id)
	}
	q := listing("-popularity")
	q.Set("filter[tags.sys.id]", ids[0])
	return s.recipeList(ctx, entitiesPath, q, limit, func(r apiRecipe) bool { return r.hasTags(ids[1:]) })
}

func listing(by string) url.Values {
	return url.Values{"filter[type]": {"recipe"}, "sort": {by}}
}

func tagID(known []apiTag, name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range known {
		if name != "" && (strings.ToLower(t.Fields.Title) == name || strings.ToLower(t.Fields.Slug) == name) {
			return t.ID, true
		}
	}
	return "", false
}

func tagTitles(known []apiTag) string {
	titles := make([]string, 0, len(known))
	for _, t := range known {
		titles = append(titles, t.Fields.Title)
	}
	sort.Strings(titles)
	return strings.Join(titles, ", ")
}

// recipeList reads recipe pages until there are `limit` that pass keep (nil keeps all).
func (s *Store) recipeList(ctx context.Context, path string, q url.Values, limit int, keep func(apiRecipe) bool) ([]model.Recipe, error) {
	limit = max(limit, 1)
	kept := func(rows []apiRecipe) []apiRecipe {
		var out []apiRecipe
		for _, r := range rows {
			if keep == nil || keep(r) {
				out = append(out, r)
			}
		}
		return out
	}
	rows, err := pages[apiRecipe](ctx, s, call{method: "GET", path: path, query: q}, "opskrifterne",
		min(max(limit, 20), recipePageSize), maxRecipePages, func(rows []apiRecipe) bool { return len(kept(rows)) >= limit })
	if err != nil {
		return nil, err
	}
	out := []model.Recipe{}
	for _, r := range kept(rows) {
		if r.Fields.Slug == "" {
			continue
		}
		out = append(out, r.summary())
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Recipe reads one recipe by its id, the slug in its web address, and picks the products to buy.
func (s *Store) Recipe(ctx context.Context, recipeID string, persons *int) (model.RecipeDetails, error) {
	if _, err := s.ready(ctx); err != nil {
		return model.RecipeDetails{}, err
	}
	slug := slugOf(recipeID)
	q := listing("-popularity")
	q.Set("filter[slug]", slug)
	q.Set("per_page", "2")
	var env envelope[[]apiRecipe]
	if slug == "" {
		return model.RecipeDetails{}, store.Errorf(recipeNotFoundMsg, recipeID)
	}
	if err := s.getJSON(ctx, call{method: "GET", path: entitiesPath, query: q}, "opskriften", &env); err != nil {
		return model.RecipeDetails{}, err
	}
	for _, r := range env.Data {
		if r.Fields.Slug == slug {
			return s.details(r, persons)
		}
	}
	return model.RecipeDetails{}, store.Errorf(recipeNotFoundMsg, recipeID)
}

// slugOf accepts the slug or a recipe address and returns the slug.
func slugOf(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.Index(id, "/opskrifter/"); i >= 0 {
		id = id[i+len("/opskrifter/"):]
	}
	id, _, _ = strings.Cut(id, "?")
	id, _, _ = strings.Cut(id, "#")
	return strings.Trim(id, "/ ")
}
