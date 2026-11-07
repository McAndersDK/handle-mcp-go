package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// fakeStore answers from memory, like FakeStore in tests/conftest.py. The wire goldens were
// recorded against it. The hooks stand in for Python's monkeypatch.
type fakeStore struct {
	name   string
	broken bool // BrokenStore: search fails, orders is not implemented
	mu     sync.Mutex
	cart   []store.CartItem
	rec    *recorder

	noItems   bool // the recipe has no ingredients (args.json)
	searchFn  func(query string, limit int) ([]model.Product, error)
	ordersFn  func(limit int) ([]model.Order, error)
	recipesFn func(query string, limit int) ([]model.Recipe, error)
	cartFn    func() (model.Cart, error)
	slotsFn   func() []model.DeliverySlot
	setFn     func(items []store.CartItem) ([]model.QuantityResult, error)
}

func newFake(name string) *fakeStore {
	return &fakeStore{name: name, cart: []store.CartItem{{ProductID: "1", Quantity: 2}}}
}

func newBroken() *fakeStore { f := newFake("broken"); f.broken = true; return f }

// recorder notes what the fake saw, for the args.json probes.
type recorder struct {
	mu    sync.Mutex
	items []string
}

func (r *recorder) add(format string, a ...any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, fmt.Sprintf(format, a...))
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

func kr(cents int64) model.Money { return model.NewMoney(cents, -2) }

func ptr[T any](v T) *T { return &v }

func naive(y int, m time.Month, d, h int) model.DateTime {
	return model.NaiveDateTime(time.Date(y, m, d, h, 0, 0, 0, time.UTC))
}

func (f *fakeStore) Name() string               { return f.name }
func (f *fakeStore) Session() map[string]string { return map[string]string{"token": "session-1"} }

func (f *fakeStore) product(name string) model.Product {
	p := model.NewProduct("fake", "1", name, kr(1295))
	p.UnitPrice, p.Unit, p.EAN = ptr(kr(1295)), ptr("l"), ptr("5701234567890")
	return p
}

func (f *fakeStore) Search(_ context.Context, query string, limit int) ([]model.Product, error) {
	f.rec.add("%s q=%s limit=%d", f.name, pyRepr(query), limit)
	if f.searchFn != nil {
		return f.searchFn(query, limit)
	}
	if f.broken {
		return nil, store.Errorf("API'et svarer 500")
	}
	return []model.Product{f.product(query + " 1 l")}, nil
}

func (f *fakeStore) Offers(ctx context.Context, limit int) ([]model.Product, error) {
	f.rec.add("%s offers", f.name)
	return f.Search(ctx, "Tilbud", limit)
}

func (f *fakeStore) GetCart(context.Context) (model.Cart, error) {
	if f.cartFn != nil {
		return f.cartFn()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	total := kr(0)
	lines := []model.CartLine{}
	for _, it := range f.cart {
		lt := kr(1295).Mul(int64(it.Quantity))
		lines = append(lines, model.CartLine{ProductID: it.ProductID, Name: "Mælk", Quantity: it.Quantity, LineTotal: lt})
		total = total.Add(lt)
	}
	minimum := kr(50000)
	return model.Cart{Store: "fake", Lines: lines, ProductsTotal: total, Fees: model.Dict[model.Money]{"levering": kr(3900)},
		Total: total.Add(kr(3900)), MinimumOrder: &minimum, MinimumMet: ptr(total.Cmp(minimum) >= 0),
		DeliveryMode: ptr(model.DeliveryHome)}, nil
}

func (f *fakeStore) SetQuantities(_ context.Context, items []store.CartItem) ([]model.QuantityResult, error) {
	if f.setFn != nil {
		return f.setFn(items)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []model.QuantityResult
	for _, it := range items {
		f.rec.add("qty=%d", it.Quantity)
		updated := false
		for i := range f.cart {
			if f.cart[i].ProductID == it.ProductID {
				f.cart[i].Quantity, updated = it.Quantity, true
			}
		}
		if !updated {
			f.cart = append(f.cart, it)
		}
		results = append(results, model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity, Applied: ptr(it.Quantity), OK: true})
	}
	kept := f.cart[:0]
	for _, it := range f.cart {
		if it.Quantity != 0 {
			kept = append(kept, it)
		}
	}
	f.cart = kept
	return results, nil
}

func (f *fakeStore) DeliveryInfo(context.Context) (model.DeliveryInfo, error) {
	d, _ := model.NewDate(2026, 11, 2)
	return model.DeliveryInfo{Store: "fake", ChosenStart: ptr(naive(2026, 11, 3, 16)), ChosenEnd: ptr(naive(2026, 11, 3, 19)),
		NextAvailable: &d, DeliveryMode: ptr(model.DeliveryHome)}, nil
}

func (f *fakeStore) DeliverySlots(_ context.Context, days int) ([]model.DeliverySlot, error) {
	f.rec.add("days=%d", days)
	if f.slotsFn != nil {
		return f.slotsFn(), nil
	}
	slot := func(start, end model.DateTime, price int64, kind string, free bool) model.DeliverySlot {
		s := model.NewDeliverySlot("fake", start, end, ptr(kr(price)))
		s.Available, s.Kind = free, ptr(kind)
		return s
	}
	tue, wed := naive(2026, 11, 3, 14), naive(2026, 11, 4, 14)
	return []model.DeliverySlot{
		slot(tue, naive(2026, 11, 3, 21), 2500, "Fleksibel", true),
		slot(tue, naive(2026, 11, 3, 21), 1900, "Personlig", true),
		slot(wed, naive(2026, 11, 4, 21), 0, "Personlig", true),
		slot(wed, naive(2026, 11, 4, 17), 0, "", false),
	}, nil
}

func (f *fakeStore) Terms(context.Context) (model.StoreTerms, error) {
	t := model.NewStoreTerms("fake", true)
	t.DeliveryFee, t.MinimumOrder, t.DeliveryMode = ptr(kr(3900)), ptr(kr(50000)), ptr(model.DeliveryHome)
	return t, nil
}

func (f *fakeStore) Recipes(_ context.Context, query string, limit int) ([]model.Recipe, error) {
	if f.recipesFn != nil {
		return f.recipesFn(query, limit)
	}
	return []model.Recipe{{Store: "fake", ID: "r1", Name: query + " til 4", URL: "https://example.com/r1"}}, nil
}

func (f *fakeStore) Recipe(_ context.Context, id string, persons *int) (model.RecipeDetails, error) {
	if persons == nil {
		f.rec.add("persons=None")
	} else {
		f.rec.add("persons=%d", *persons)
	}
	p := 4
	if persons != nil {
		p = *persons
	}
	milk := model.NewRecipeProduct("1", "Mælk", kr(1295), 2)
	milk.Size = ptr("1 l")
	items := model.List[model.RecipeItem]{{Ingredient: "mælk", Product: milk}}
	if f.noItems {
		items = nil
	}
	return model.RecipeDetails{Store: "fake", ID: id, Name: "Grød", URL: "https://example.com/r1", Persons: &p,
		Items: items, Total: kr(2590)}, nil
}

func (f *fakeStore) RecipesByTag(context.Context, []string, *string, int) ([]model.Recipe, error) {
	return []model.Recipe{{Store: "fake", ID: "r2", Name: "Pasta", URL: "https://example.com/r2", Group: ptr("Aftensmad")}}, nil
}

func (f *fakeStore) FeaturedRecipes(context.Context, int) ([]model.Recipe, error) {
	return []model.Recipe{{Store: "fake", ID: "r1", Name: "Grød", URL: "https://example.com/r1", Group: ptr("Morgen")}}, nil
}

func (f *fakeStore) Orders(_ context.Context, limit int) ([]model.Order, error) {
	f.rec.add("limit=%d", limit)
	if f.ordersFn != nil {
		return f.ordersFn(limit)
	}
	if f.broken {
		return nil, store.ErrNotImplemented
	}
	return []model.Order{{Store: "fake", ID: "900000000001", Status: "Leveret", Total: kr(61250)}}, nil
}

func (f *fakeStore) Order(_ context.Context, id string) (model.Order, error) {
	f.rec.add("order_id=%s", id)
	return model.Order{Store: "fake", ID: id, Status: "Leveret", Total: kr(61250),
		Lines: model.List[model.CartLine]{{ProductID: "1", Name: "Mælk", Quantity: 2, LineTotal: kr(2590)}}}, nil
}

var _ store.Store = (*fakeStore)(nil)

// pyRepr is Python's repr of a plain string, enough for the probes: single quotes.
func pyRepr(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }
