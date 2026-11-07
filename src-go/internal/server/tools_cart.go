package server

import (
	"context"
	"errors"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/recipecart"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

func (s *Server) cart(ctx context.Context, name string) (model.Cart, error) {
	return call(ctx, s, name, func(ctx context.Context, st store.Store) (model.Cart, error) { return st.GetCart(ctx) })
}

func (s *Server) setQuantities(ctx context.Context, name string, items []store.CartItem) ([]model.QuantityResult, error) {
	return call(ctx, s, name, func(ctx context.Context, st store.Store) ([]model.QuantityResult, error) {
		return st.SetQuantities(ctx, items)
	})
}

func (s *Server) cartTools() map[string]toolFunc {
	return map[string]toolFunc{
		"compare_basket":      s.compareBasket,
		"add_recipe_to_cart":  s.addRecipe,
		"set_cart_quantities": s.setCartQuantities,
		"get_cart": func(ctx context.Context, a Args) (any, error) {
			if name := a.OptStr("store"); name != nil {
				return s.cart(ctx, *name)
			}
			names, err := s.names(nil)
			if err != nil {
				return nil, err
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (model.Cart, error) { return st.GetCart(ctx) }), nil
		},
	}
}

func (s *Server) compareBasket(ctx context.Context, a Args) (any, error) {
	var items []string
	for _, it := range a.List("items") {
		if t := pystr.Strip(it); t != "" {
			items = append(items, t)
		}
	}
	if len(items) == 0 {
		return nil, toolErrorf("Indkøbslisten er tom.")
	}
	if len(items) > MaxItems {
		return nil, toolErrorf("Højst %d varer pr. liste.", MaxItems)
	}
	names, err := s.names(a.List("stores"))
	if err != nil {
		return nil, err
	}
	results := gather(ctx, s, names, func(ctx context.Context, st store.Store) (compare.BasketQuote, error) {
		return compare.Quote(ctx, st, items)
	})
	return compareAnswer{Stores: entries(results), VerdictAnswer: compare.Verdict(results)}, nil
}

// setCartQuantities sets absolute counts (0 removes a line). It never orders or pays.
func (s *Server) setCartQuantities(ctx context.Context, a Args) (any, error) {
	pairs := a.IntPairs("items")
	if len(pairs) == 0 {
		return nil, toolErrorf("Ingen varer angivet.")
	}
	if len(pairs) > MaxItems {
		return nil, toolErrorf("Højst %d varer pr. kald.", MaxItems)
	}
	var wrong []string
	items := make([]store.CartItem, len(pairs))
	for i, p := range pairs {
		if p.N < 0 || p.N > MaxQuantity {
			wrong = append(wrong, p.Key)
		}
		items[i] = store.CartItem{ProductID: p.Key, Quantity: p.N}
	}
	if len(wrong) > 0 {
		return nil, toolErrorf("Antal skal være 0-%d. Forkert for: %s.", MaxQuantity, strings.Join(wrong, ", "))
	}
	results, err := s.setQuantities(ctx, a.Str("store"), items)
	if err != nil {
		return nil, err
	}
	cart, err := s.cart(ctx, a.Str("store"))
	return setAnswer{Results: lst(results), Cart: cart}, err
}

// addRecipe puts a recipe's products in the cart as absolute counts: what is there plus the
// recipe's packs. The store's own "add recipe" call is never used.
func (s *Server) addRecipe(ctx context.Context, a Args) (any, error) {
	name := a.Str("store")
	details, err := call(ctx, s, name, func(ctx context.Context, st store.Store) (model.RecipeDetails, error) {
		return st.Recipe(ctx, a.Str("recipe"), personsOf(a.OptInt("persons")))
	})
	if err != nil {
		return nil, err
	}
	var swaps []recipecart.Swap
	for _, p := range a.StrPairs("swap") {
		swaps = append(swaps, recipecart.Swap{Key: p.Key, Value: p.Value})
	}
	lines, skipped, err := recipecart.Plan(details, a.List("skip"), swaps, a.List("include"))
	var planErr *recipecart.PlanError
	if errors.As(err, &planErr) {
		return nil, toolErrorf("%s", planErr.Msg)
	}
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, toolErrorf("Ingen varer at lægge i kurven.")
	}
	if len(lines) > MaxItems {
		return nil, toolErrorf("Højst %d varer pr. kald.", MaxItems)
	}
	current, err := s.cart(ctx, name)
	if err != nil {
		return nil, err
	}
	before := recipecart.InCart(current)
	targets := make([]store.CartItem, len(lines))
	var tooMany []string
	for i, ln := range lines {
		targets[i] = store.CartItem{ProductID: ln.ProductID, Quantity: before[ln.ProductID] + ln.Quantity}
		if targets[i].Quantity > MaxQuantity {
			tooMany = append(tooMany, ln.ProductID)
		}
	}
	if len(tooMany) > 0 {
		return nil, toolErrorf("Højst %d stk. pr. vare. For mange af: %s.", MaxQuantity, strings.Join(tooMany, ", "))
	}
	results, err := s.setQuantities(ctx, name, targets)
	if err != nil {
		return nil, err
	}
	added := recipecart.Report(lines, before, results)
	cart, err := s.cart(ctx, name)
	return addAnswer{Added: lst(added), Skipped: lst(skipped), Cart: cart}, err
}
