package bilkatogo

// Read-only smoke tests against the real BilkaToGo: the BilkaToGo parts of tests/test_live.py.
//
// Run, in PowerShell, from the repo root (the password is typed, never put in a file):
//
//	$env:HANDLE_LIVE = "1"
//	$env:HANDLE_BILKATOGO_EMAIL = Read-Host "BilkaToGo email"
//	$pw = Read-Host "BilkaToGo password" -AsSecureString
//	$env:HANDLE_BILKATOGO_PASSWORD = [System.Net.NetworkCredential]::new("", $pw).Password
//	go test -count=1 -v -run Live ./src-go/internal/store/bilkatogo/
//	Remove-Item Env:HANDLE_LIVE, Env:HANDLE_BILKATOGO_EMAIL, Env:HANDLE_BILKATOGO_PASSWORD
//
// Or have run `handle-mcp login bilkatogo` (a saved session is read from the keychain; with
// HANDLE_LIVE=1 this package does not swap the keychain for a memory one).
//
// These tests NEVER change the cart: the store the tests see has no working SetQuantities, and
// the HTTP client refuses everything but GET (not ChangeLineCount, which is a GET that writes),
// the login POSTs, the Algolia search POST and the order list POST, which only reads.
//
// The account the author tests with is pickup only. Until pickup is supported, delivery slots on
// it answer NOT_HOME; the slots test accepts exactly that and logs that the home paths are not
// proven by this run.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
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

// readOnly refuses every request that could change something at BilkaToGo.
type readOnly struct {
	tb    errorer
	inner http.RoundTripper
}

func (r readOnly) allowed(req *http.Request) bool {
	path := req.URL.Path
	if strings.Contains(path, "ChangeLineCount") {
		return false // a GET, and the one that writes
	}
	switch req.Method {
	case http.MethodGet:
		return true
	case http.MethodPost:
		switch {
		case req.URL.Host == "accounts.eu1.gigya.com": // the login, and the JWT for it
			return path == "/accounts.login" || path == "/accounts.getJWT"
		case req.URL.Host == "api.bilkatogo.dk":
			return path == "/api/auth/LoginJWT" || path == "/api/shop/v7/OrderHistory" // the list is a POST that reads
		case strings.HasSuffix(req.URL.Host, ".algolia.net"):
			return path == "/1/indexes/*/queries" // the search
		}
	}
	return false
}

func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if !r.allowed(req) {
		r.tb.Errorf("a live test must only read: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("live tests only read")
	}
	return r.inner.RoundTrip(req)
}

func liveStore(t *testing.T) store.Store {
	t.Helper()
	if os.Getenv("HANDLE_LIVE") != "1" {
		t.Skip("set HANDLE_LIVE=1 to run against the real BilkaToGo")
	}
	s := New(&http.Client{Transport: readOnly{tb: t, inner: http.DefaultTransport}})
	return noWrites{Store: s, tb: t}
}

// show prints the value a pointer points to, and "-" for nil. A pointer given to %v prints as a
// hex address, which tells the reader nothing.
func show[T any](p *T) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
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
		t.Logf("%s kr/%s | %s %s | %s kr", show(p.UnitPrice), show(p.Unit), p.Name, show(p.Size), p.Price)
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
	t.Logf("hjemlevering=%v gebyrer=%v linjer=%d", terms.HomeDelivery, cart.Fees, len(cart.Lines))
	t.Logf("minimum=%s nået=%s advarsler=%v", show(cart.MinimumOrder), show(cart.MinimumMet), cart.Warnings)
	t.Logf("valgt tid=%s-%s næste ledige=%s note=%s", show(info.ChosenStart), show(info.ChosenEnd),
		show(info.NextAvailable), show(info.Note))
}

// GET only: lists slots, never chooses one. A pickup cart answers NOT_HOME (pickup is not
// supported yet); that is accepted, and the run says the home paths were not proven.
func TestLiveDeliverySlots(t *testing.T) {
	s := liveStore(t)
	slots, err := s.DeliverySlots(context.Background(), 3)
	var se *store.StoreError
	if errors.As(err, &se) && se.Msg == notHome {
		t.Log("NOT_HOME: this account is on pickup, so the home-delivery slot paths are NOT proven by this run")
		return
	}
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
		t.Logf("%v-%v %s %s kr", sl.Start, sl.End, show(sl.Kind), show(sl.Price))
	}
}

// A list can be empty on a given day, so this only checks what is there.
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
		if p.Price.Sign() <= 0 || p.Price.Cmp(model.NewMoney(2000, 0)) >= 0 {
			t.Errorf("odd price %s for %s", p.Price, p.Name)
		}
		if p.BeforePrice != nil && p.BeforePrice.Cmp(p.Price) <= 0 {
			t.Errorf("before price %s is not above %s", p.BeforePrice, p.Price)
		}
	}
	t.Logf("%d tilbud, lister: %v", len(offers), lists)
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
	h := newHarness(t)
	probe := &recorder{} // what the guards report is recorded here, not on the real test
	guard := readOnly{tb: probe, inner: h.f}
	for _, c := range []struct{ method, url string }{
		{"GET", "https://api.bilkatogo.dk/api/shop/v6/ChangeLineCount?productId=1&count=1"},
		{"POST", "https://api.bilkatogo.dk/api/shop/v6/ChangeLineCount"},
		{"POST", "https://api.bilkatogo.dk/api/shop/v6/Cart"},
		{"PUT", "https://api.bilkatogo.dk/api/shop/v6/Cart"},
		{"DELETE", "https://api.bilkatogo.dk/api/shop/v6/Cart"},
		{"POST", "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/other"},
		{"POST", "https://accounts.eu1.gigya.com/accounts.setAccountInfo"},
	} {
		req, _ := http.NewRequest(c.method, c.url, nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Errorf("%s %s got through the live guard", c.method, c.url)
		}
	}
	for _, c := range []struct{ method, url string }{
		{"GET", "https://api.bilkatogo.dk/api/shop/v6/Cart"},
		{"POST", "https://accounts.eu1.gigya.com/accounts.login"},
		{"POST", "https://accounts.eu1.gigya.com/accounts.getJWT"},
		{"POST", "https://api.bilkatogo.dk/api/auth/LoginJWT"},
		{"POST", "https://api.bilkatogo.dk/api/shop/v7/OrderHistory"},
		{"POST", "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/*/queries"},
	} {
		req, _ := http.NewRequest(c.method, c.url, strings.NewReader(`{"requests":[{"params":"query=x&filters=y"}]}`))
		if _, err := guard.RoundTrip(req); err != nil {
			t.Errorf("%s %s was stopped: %v", c.method, c.url, err)
		}
	}
	wrapped := noWrites{Store: New(nil), tb: probe}
	if _, err := wrapped.SetQuantities(context.Background(), []store.CartItem{{ProductID: "1", Quantity: 1}}); err == nil {
		t.Error("SetQuantities must fail in a live test")
	}
	if len(probe.reports) < 8 { // seven writes through the transport, one through the store
		t.Errorf("the guards must report every write as a test failure: %v", probe.reports)
	}
}

func TestShowPrintsValuesNotAddresses(t *testing.T) {
	s, b := "kg", true
	if got := show(&s) + show(&b) + show[string](nil); got != "kgtrue-" {
		t.Errorf("show = %q", got)
	}
}

// BilkaToGo recipes: public GETs of bilkatogo.dk/inspiration/opskrifter/ and the page data
// (/_nuxt/static/<build>/.../payload.js), and the Algolia search that prices the products. The
// prices follow the address's store, so this needs the login too. Port of the two BilkaToGo
// recipe tests of tests/test_live.py.
//
//	go test -count=1 -v -run 'LiveMadplan|LiveRecipesByCategory' ./src-go/internal/store/bilkatogo/
func TestLiveMadplanRecipeHasProducts(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	week, err := s.FeaturedRecipes(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(week))
	for i, r := range week {
		names[i] = r.Name
	}
	if len(week) < 5 {
		t.Fatalf("the madplan has %d recipes: %v", len(week), names)
	}
	recipe, err := s.Recipe(ctx, week[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %v", show(week[0].Group), names)
	t.Logf("%s | %s pers. | %s kr | missing %v", recipe.Name, show(recipe.Persons), recipe.Total, recipe.Missing)
	for _, it := range recipe.Items {
		t.Logf("  %s: %s %s kr", it.Product.ID, it.Product.Name, it.Product.Price)
	}
	if len(recipe.Ingredients) == 0 || len(recipe.Method) == 0 {
		t.Errorf("no text: %d ingredients, %d method parts", len(recipe.Ingredients), len(recipe.Method))
	}
	if len(recipe.Items) == 0 || recipe.Total.Sign() <= 0 { // the item numbers are found in Algolia
		t.Errorf("no products: %d items, total %s", len(recipe.Items), recipe.Total)
	}
}

func TestLiveRecipesByCategory(t *testing.T) {
	s := liveStore(t)
	category := "aftensmad"
	recipes, err := s.RecipesByTag(context.Background(), nil, &category, 10)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]bool{}
	for _, r := range recipes {
		t.Logf("%s: %s | %s", show(r.Group), r.Name, r.URL)
		groups[show(r.Group)] = true
	}
	if len(groups) <= 3 { // mixed across sub-categories
		t.Errorf("only %d groups: %v", len(groups), groups)
	}
}

// The recipe pages are plain GETs: the live guard lets them through and still refuses to write there.
func TestLiveGuardAllowsTheRecipePagesOnly(t *testing.T) {
	isolate(t)
	probe := &recorder{}
	guard := readOnly{tb: probe, inner: newSiteFake(newFake(t))}
	for _, u := range []string{
		"https://www.bilkatogo.dk/inspiration/opskrifter/",
		"https://www.bilkatogo.dk/_nuxt/static/1793920097/inspiration/opskrifter/madplaner/payload.js",
	} {
		req, _ := http.NewRequest("GET", u, nil)
		if _, err := guard.RoundTrip(req); err != nil {
			t.Errorf("GET %s was stopped: %v", u, err)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req, _ := http.NewRequest(method, "https://www.bilkatogo.dk/inspiration/opskrifter/", strings.NewReader("x"))
		if _, err := guard.RoundTrip(req); err == nil {
			t.Errorf("%s to the website got through the live guard", method)
		}
	}
	if len(probe.reports) != 4 {
		t.Errorf("each write must be reported once: %v", probe.reports)
	}
}
