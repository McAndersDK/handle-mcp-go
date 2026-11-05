package nemlig

import (
	"context"
	"encoding/json"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

var (
	jsonOne = json.Number("1")
	json0   = json.Number("0")
)

// Search is the old search gateway. It needs a login: with an anonymous token it answers 500
// (live 2026-11-04), so say so up front.
func (s *Store) Search(ctx context.Context, query string, limit int) (_ []model.Product, err error) {
	defer guardAs(&err, "nemlig-søgning har ændret format.")
	if !s.hasLogin(ctx) {
		return nil, store.Errorf("nemlig-søgning kræver login (tilbud gør ikke). Gem et login: handle-mcp login nemlig")
	}
	settings, err := s.searchSettings(ctx)
	if err != nil {
		return nil, err
	}
	p := params{}.add("query", query).add("take", limit).add("skip", 0).add("recipeCount", 0)
	data, err := s.api(ctx, call{method: "GET", url: searchURL, params: settings.addTo(p)})
	if err != nil {
		return nil, err
	}
	hits, ok := hitsOf(data)
	if !ok {
		return nil, store.Errorf("nemlig-søgning har ændret format.")
	}
	return parseHits(hits), nil
}

// hitsOf is data["Products"]["Products"], a list.
func hitsOf(data any) ([]any, bool) {
	top, ok := data.(obj)
	if !ok {
		return nil, false
	}
	inner, ok := top["Products"].(obj)
	if !ok {
		return nil, false
	}
	hits, ok := inner["Products"].([]any)
	return hits, ok
}

// parseHits keeps nemlig's order. A hit without a price is a format change, not a free product.
func parseHits(hits []any) []model.Product {
	out := make([]model.Product, 0, len(hits))
	for _, h := range hits {
		out = append(out, product(asObj(h)))
	}
	return out
}
