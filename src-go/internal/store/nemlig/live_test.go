package nemlig

// Read-only smoke tests against the real nemlig.com: the nemlig parts of tests/test_live.py.
//
// Run (needs a login, see below):
//
//	HANDLE_LIVE=1 go test -count=1 -v -run Live ./src-go/internal/store/nemlig/
//
// Login: set HANDLE_NEMLIG_EMAIL and HANDLE_NEMLIG_PASSWORD, or have run `handle-mcp login nemlig`
// (a saved session is read from the keychain; with HANDLE_LIVE=1 this package does not swap the
// keychain for a memory one). These tests NEVER change the cart: the store the tests see has no
// working SetQuantities, and the HTTP client refuses every request that is not a GET, except
// the login itself.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/recipecart"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// errorer is what the guards need of a test: they report to it.
type errorer interface {
	Errorf(format string, args ...any)
}

// recorder collects what a guard reports, for the offline test of the guards.
type recorder struct{ reports []string }

func (r *recorder) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

// noWrites is the store with SetQuantities made to fail, as the Python no_writes monkeypatch did.
type noWrites struct {
	store.Store
	tb errorer
}

func (n noWrites) SetQuantities(context.Context, []store.CartItem) ([]model.QuantityResult, error) {
	n.tb.Errorf("a live test must never write to the cart")
	return nil, errors.New("live tests never write to the cart")
}

// readOnly refuses every request that could change something at nemlig: only GET, and the login
// POST, which sets the login cookie and nothing in the cart.
type readOnly struct {
	tb    errorer
	inner http.RoundTripper
}

func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && !(req.Method == http.MethodPost && req.URL.Path == "/webapi/login") {
		r.tb.Errorf("a live test must only read: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("live tests only read")
	}
	return r.inner.RoundTrip(req)
}

func liveStore(t *testing.T) store.Store {
	t.Helper()
	if os.Getenv("HANDLE_LIVE") != "1" {
		t.Skip("set HANDLE_LIVE=1 to run against the real nemlig.com")
	}
	s := New(&http.Client{Transport: readOnly{tb: t, inner: http.DefaultTransport}})
	return noWrites{Store: s, tb: t}
}

func TestLiveSearch(t *testing.T) {
	s := liveStore(t)
	products, err := s.Search(context.Background(), "minimælk", 3)
	if err != nil || len(products) == 0 {
		t.Fatalf("search: %v (%d products)", err, len(products))
	}
	for _, p := range products {
		if p.Price.Sign() <= 0 {
			t.Errorf("%s costs %s", p.Name, p.Price)
		}
	}
}

// Stores rank by relevance; sorted search must still find the cheap packs far down.
func TestLiveCheapestFirst(t *testing.T) {
	s := liveStore(t)
	hits, err := compare.Search(context.Background(), s, "kyllingebryst", 5, "unit_price")
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v (%d hits)", err, len(hits))
	}
	var prev *model.Money
	for _, p := range hits {
		t.Logf("%v kr/%v | %s %v | %s kr", show(p.UnitPrice), show(p.Unit), p.Name, show(p.Size), p.Price)
		if p.UnitPrice == nil {
			continue
		}
		if prev != nil && p.UnitPrice.Cmp(*prev) < 0 {
			t.Errorf("not sorted by unit price: %s after %s", p.UnitPrice, prev)
		}
		prev = p.UnitPrice
	}
}

func TestLiveCartTermsDelivery(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	cart, err := s.GetCart(ctx)
	if err != nil || cart.Total.Sign() < 0 {
		t.Fatalf("cart: %v (total %s)", err, cart.Total)
	}
	terms, err := s.Terms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.DeliveryInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Printed so a human can check the facts (no personal data in these fields).
	t.Logf("hjemlevering=%v gebyrer=%v", terms.HomeDelivery, cart.Fees)
	t.Logf("minimum=%v nået=%v advarsler=%v", show(cart.MinimumOrder), show(cart.MinimumMet), cart.Warnings)
	t.Logf("valgt tid=%v-%v", show(info.ChosenStart), show(info.ChosenEnd))
}

// GET only: lists slots, never chooses one.
func TestLiveDeliverySlots(t *testing.T) {
	s := liveStore(t)
	slots, err := s.DeliverySlots(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	var free []model.DeliverySlot
	for _, sl := range slots {
		if sl.Available {
			free = append(free, sl)
		}
	}
	if len(free) == 0 {
		t.Fatal("no available slots")
	}
	for _, sl := range free {
		if !sl.End.Time().After(sl.Start.Time()) {
			t.Errorf("slot ends before it starts: %v-%v", sl.Start, sl.End)
		}
	}
	for _, sl := range free[:min(12, len(free))] {
		t.Logf("%v-%v %v %v kr", sl.Start, sl.End, show(sl.Kind), show(sl.Price))
	}
}

// A list can be empty on a given day (Offers leaves it out), so only one of the big weekly lists
// is needed.
func TestLiveOffers(t *testing.T) {
	s := liveStore(t)
	offers, err := s.Offers(context.Background(), 10)
	if err != nil || len(offers) == 0 {
		t.Fatalf("offers: %v (%d)", err, len(offers))
	}
	lists := map[string]bool{}
	for _, p := range offers {
		if p.Campaign != nil {
			lists[*p.Campaign] = true
		}
		if !p.OnOffer || p.ID == "" || p.Name == "" {
			t.Errorf("not an offer: %+v", p)
		}
		if (p.Campaign == nil || *p.Campaign != "Gratis") && (p.Price.Sign() <= 0 || p.Price.Cmp(model.NewMoney(2000, 0)) >= 0) {
			t.Errorf("odd price %s for %s", p.Price, p.Name)
		}
		if p.BeforePrice != nil && p.BeforePrice.Cmp(p.Price) <= 0 {
			t.Errorf("before price %s is not above %s", p.BeforePrice, p.Price)
		}
	}
	t.Logf("%d tilbud, lister: %v", len(offers), lists)
	if !lists["Skarp pris"] && !lists["Priskup"] {
		t.Error("neither Skarp pris nor Priskup")
	}
}

// add_recipe_to_cart's plan on live data: recipe and cart are read, nothing is written.
func TestLiveAddRecipeDryRun(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	featured, err := s.FeaturedRecipes(ctx, 1)
	if err != nil || len(featured) == 0 {
		t.Fatalf("featured: %v", err)
	}
	recipe, err := s.Recipe(ctx, featured[0].URL, intPtr(4))
	if err != nil {
		t.Fatal(err)
	}
	lines, skipped, err := recipecart.Plan(recipe, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, err := s.GetCart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := recipecart.InCart(cart)
	t.Logf("%s: %d varer, %d sprunget over", recipe.Name, len(lines), len(skipped))
	if len(lines) == 0 {
		t.Fatal("no products in the plan")
	}
	for _, ln := range lines {
		if target := before[ln.ProductID] + ln.Quantity; target < 1 || target > 50 {
			t.Errorf("%s: target %d", ln.Name, target)
		}
	}
	for _, sk := range skipped {
		if sk.Reason == "" {
			t.Errorf("skipped without a reason: %+v", sk)
		}
	}
}

func intPtr(n int) *int { return &n }

func TestLiveRecipes(t *testing.T) {
	s := liveStore(t)
	recipes, err := s.Recipes(context.Background(), "kylling karry", 3)
	if err != nil || len(recipes) == 0 {
		t.Fatalf("recipes: %v (%d)", err, len(recipes))
	}
	for _, r := range recipes {
		t.Logf("%s | %s", r.Name, r.URL)
		if !strings.HasPrefix(r.URL, "https://www.nemlig.com/") {
			t.Errorf("odd link %s", r.URL)
		}
	}
}

func TestLiveFeaturedRecipeHasProducts(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	featured, err := s.FeaturedRecipes(ctx, 1)
	if err != nil || len(featured) == 0 {
		t.Fatalf("featured: %v", err)
	}
	recipe, err := s.Recipe(ctx, featured[0].URL, intPtr(6))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v: %s | %s pers. | %s kr", show(featured[0].Group), recipe.Name, show(recipe.Persons), recipe.Total)
	for _, it := range recipe.Items {
		t.Logf("  home=%v %s: %s x%d", it.HaveAtHome, it.Ingredient, it.Product.Name, it.Product.Quantity)
	}
	if recipe.Persons == nil || *recipe.Persons != 6 || recipe.Total.Sign() <= 0 {
		t.Errorf("persons %s, total %s", show(recipe.Persons), recipe.Total)
	}
	if len(recipe.Method) == 0 {
		t.Error("no method")
	}
	for _, m := range recipe.Method {
		if len(m.Steps) == 0 {
			t.Error("a method section without steps")
		}
	}
	if recipe.Image != nil && !strings.HasPrefix(*recipe.Image, "https://") {
		t.Errorf("image %s", *recipe.Image)
	}
	if p := recipe.IngredientsPersons; p != nil && (*p < 1 || *p > 12) {
		t.Errorf("ingredients_persons %d", *p)
	}
	if !slices.ContainsFunc(recipe.Items, func(i model.RecipeItem) bool { return !i.HaveAtHome }) {
		t.Error("nothing to buy")
	}
}

func TestLiveRecipesByTag(t *testing.T) {
	s := liveStore(t)
	recipes, err := s.RecipesByTag(context.Background(), []string{"Familievenlig", "Under 30 min"}, nil, 5)
	if err != nil || len(recipes) == 0 {
		t.Fatalf("by tag: %v (%d)", err, len(recipes))
	}
	for _, r := range recipes {
		t.Logf("%v: %s | %s", show(r.Group), r.Name, r.URL)
		if !strings.HasPrefix(r.URL, "https://www.nemlig.com/maaltider/") {
			t.Errorf("odd link %s", r.URL)
		}
	}
}

func TestLiveOrders(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	orders, err := s.Orders(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) > 0 {
		if _, err := s.Order(ctx, orders[0].ID); err != nil {
			t.Fatal(err)
		}
	}
}

// The guards themselves are tested offline: a write must fail, whatever the test does.
func TestLiveGuardsStopWrites(t *testing.T) {
	isolate(t)
	h, _ := newHarness(t)
	probe := &recorder{} // what the guards report is recorded here, not on the real test
	guard := readOnly{tb: probe, inner: h.f}
	for _, c := range []struct{ method, path string }{
		{"POST", "/webapi/basket/AddToBasket"}, {"PUT", "/webapi/x"}, {"DELETE", "/webapi/x"},
	} {
		req, _ := http.NewRequest(c.method, "https://www.nemlig.com"+c.path, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Errorf("%s %s got through the live guard", c.method, c.path)
		}
	}
	for _, c := range []struct{ method, path string }{{"GET", "/webapi/Token"}, {"POST", "/webapi/login"}} {
		body := strings.NewReader(`{"Password": "x"}`)
		req, _ := http.NewRequest(c.method, "https://www.nemlig.com"+c.path, body)
		if _, err := guard.RoundTrip(req); err != nil {
			t.Errorf("%s %s was stopped: %v", c.method, c.path, err)
		}
	}
	wrapped := noWrites{Store: New(nil), tb: probe}
	if _, err := wrapped.SetQuantities(context.Background(), []store.CartItem{{ProductID: "1", Quantity: 1}}); err == nil {
		t.Error("SetQuantities must fail in a live test")
	}
	if len(probe.reports) < 4 { // three writes through the transport, one through the store
		t.Errorf("the guards must report every write as a test failure: %v", probe.reports)
	}
}

// show prints the value a pointer points to, and "-" for nil. A pointer given to %v prints as a
// hex address, which tells the reader nothing.
func show[T any](p *T) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
}

func TestShowPrintsValuesNotAddresses(t *testing.T) {
	s, b := "kg", true
	if got := show(&s) + show(&b) + show[string](nil); got != "kgtrue-" {
		t.Errorf("show = %q", got)
	}
}
