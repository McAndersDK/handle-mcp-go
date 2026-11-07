package rema1000

// Smoke tests against the real REMA 1000.
//
// Read-only (needs a login and a chosen pickup store; sends no write at all):
//
//	$env:HANDLE_LIVE = "1"
//	go test -count=1 -v -run Live ./internal/store/rema1000/
//
// A login is a session saved by `handle-mcp login rema1000` (keychain; with HANDLE_LIVE=1 this
// package does not swap the keychain for a memory one), or HANDLE_REMA1000_EMAIL and
// HANDLE_REMA1000_PASSWORD plus HANDLE_REMA1000_STORE_ID. The guard below refuses every request
// that is not a GET, except the login and the token renewal; they set no list, store or time.
//
// The write test is separate and needs the human's explicit yes. It creates today's list
// "handle-mcp <date>" if it is missing, sets one product to 1 and back, and checks that the
// human's own lists did not change. It never touches another list.
//
//	$env:HANDLE_LIVE = "1"; $env:HANDLE_LIVE_WRITE = "1"
//	go test -count=1 -v -run LiveWrite ./internal/store/rema1000/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// errorer is what the guards need of a test.
type errorer interface {
	Errorf(format string, args ...any)
}

// readOnly refuses every request that could change something at REMA 1000: only GET, the OAuth
// login POST and the token renewal POST (they set cookies and nothing else).
type readOnly struct {
	tb    errorer
	inner http.RoundTripper
}

func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	login := req.Method == http.MethodPost && req.URL.Host == appHost && req.URL.Path == "/api/oauth2/login"
	renew := req.Method == http.MethodPost && req.URL.Host == shopHost && req.URL.Path == "/api/auth/refresh"
	if req.Method != http.MethodGet && !login && !renew {
		r.tb.Errorf("a live test must only read: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("live tests only read")
	}
	return r.inner.RoundTrip(req)
}

// noWrites is the store with SetQuantities made to fail.
type noWrites struct {
	store.Store
	tb errorer
}

func (n noWrites) SetQuantities(context.Context, []store.CartItem) ([]model.QuantityResult, error) {
	n.tb.Errorf("a live read test must never write to the cart")
	return nil, errors.New("live read tests never write")
}

func liveStore(t *testing.T) store.Store {
	t.Helper()
	if os.Getenv("HANDLE_LIVE") != "1" {
		t.Skip("set HANDLE_LIVE=1 to run against the real REMA 1000")
	}
	return noWrites{Store: New(&http.Client{Transport: readOnly{tb: t, inner: http.DefaultTransport}}), tb: t}
}

func TestLiveGuardsRefuseWrites(t *testing.T) {
	rec := &recorder{}
	ro := readOnly{tb: rec, inner: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, c := range []struct {
		method, url string
		allowed     bool
	}{
		{"GET", "https://api.digital.rema1000.dk/api/search/products", true},
		{"POST", "https://cphapp.rema1000.dk/api/oauth2/login", true},
		{"POST", "https://shop.rema1000.dk/api/auth/refresh", true},
		{"POST", "https://api.digital.rema1000.dk/api/v2/shoppinglists", false},
		{"PATCH", "https://api.digital.rema1000.dk/api/v2/shoppinglists/1", false},
		{"POST", "https://api.digital.rema1000.dk/api/v2/shoppinglists/1/items", false},
	} {
		req, _ := http.NewRequest(c.method, c.url, nil)
		before := len(rec.reports)
		_, err := ro.RoundTrip(req)
		if c.allowed != (err == nil) || c.allowed != (len(rec.reports) == before) {
			t.Errorf("%s %s: allowed=%v err=%v", c.method, c.url, c.allowed, err)
		}
	}
	nw := noWrites{tb: rec}
	if _, err := nw.SetQuantities(context.Background(), nil); err == nil {
		t.Error("noWrites must fail")
	}
}

type recorder struct{ reports []string }

func (r *recorder) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveSearchAndOffers(t *testing.T) {
	s := liveStore(t)
	products, err := s.Search(context.Background(), "minimælk", 5)
	if err != nil || len(products) == 0 {
		t.Fatalf("search: %v (%d products)", err, len(products))
	}
	for _, p := range products {
		if p.ID == "" || p.Name == "" || p.Price.Sign() <= 0 {
			t.Errorf("bad product: %+v", p)
		}
	}
	offers, err := s.Offers(context.Background(), 10)
	if err != nil || len(offers) == 0 {
		t.Fatalf("offers: %v (%d)", err, len(offers))
	}
	for _, o := range offers {
		if !o.OnOffer {
			t.Errorf("not an offer: %s", o.Name)
		}
	}
}

func TestLiveCartAndDelivery(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	cart, err := s.GetCart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cart.DeliveryMode == nil || *cart.DeliveryMode != "pickup" || cart.PickupStore == nil {
		t.Errorf("cart mode: %+v", cart)
	}
	t.Logf("cart: %d lines, total %s", len(cart.Lines), cart.Total)
	slots, err := s.DeliverySlots(ctx, 7)
	if err != nil || len(slots) == 0 {
		t.Fatalf("slots: %v (%d)", err, len(slots))
	}
	info, err := s.DeliveryInfo(ctx)
	if err != nil || info.PickupStore == nil || info.ChosenStart != nil {
		t.Fatalf("info: %+v, %v", info, err)
	}
	terms, err := s.Terms(ctx)
	if err != nil || terms.HomeDelivery {
		t.Fatalf("terms: %+v, %v", terms, err)
	}
	if _, err := s.Orders(ctx, 5); !errors.Is(err, store.ErrNotImplemented) {
		t.Errorf("orders: %v", err)
	}
}

func TestLiveRecipes(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	found, err := s.Recipes(ctx, "kylling", 3)
	if err != nil || len(found) == 0 {
		t.Fatalf("recipes: %v (%d)", err, len(found))
	}
	if feat, err := s.FeaturedRecipes(ctx, 3); err != nil || len(feat) == 0 {
		t.Errorf("featured: %v (%d)", err, len(feat))
	}
	if tagged, err := s.RecipesByTag(ctx, []string{"Aftensmad"}, nil, 3); err != nil || len(tagged) == 0 {
		t.Errorf("by tag: %v (%d)", err, len(tagged))
	}
	four := 4
	d, err := s.Recipe(ctx, found[0].ID, &four)
	if err != nil || len(d.Items) == 0 || d.Total.Sign() <= 0 {
		t.Fatalf("recipe: %v, %d items", err, len(d.Items))
	}
	t.Logf("recipe %s: %d items, %d missing, total %s", d.ID, len(d.Items), len(d.Missing), d.Total)
}

// --- the write test -----------------------------------------------------------------

var writePath = regexp.MustCompile(`^/api/v2/shoppinglists(?:/(\d+)(?:/items(?:/\d+)?)?)?$`)

// onlyToday allows writes to one place: creating today's list (not primary), and changing the
// lines of the one list whose id is set. Everything else that is not a GET is refused.
type onlyToday struct {
	tb    errorer
	inner http.RoundTripper
	name  string

	mu sync.Mutex
	id string
}

func (g *onlyToday) setID(id string) { g.mu.Lock(); g.id = id; g.mu.Unlock() }

func (g *onlyToday) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet || (req.Method == http.MethodPost &&
		(req.URL.Path == "/api/oauth2/login" || req.URL.Path == "/api/auth/refresh")) {
		return g.inner.RoundTrip(req)
	}
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body.Close()
		req.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	g.mu.Lock()
	id := g.id
	g.mu.Unlock()
	m := writePath.FindStringSubmatch(req.URL.Path)
	create := req.Method == http.MethodPost && m != nil && m[1] == "" &&
		string(body) == fmt.Sprintf(`{"name":%q,"primary":false,"items":[]}`, g.name)
	other := m != nil && m[1] != "" && m[1] == id && id != "" && (req.Method == http.MethodPost || req.Method == http.MethodPatch)
	if !create && !other {
		g.tb.Errorf("a write outside today's list: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("refused by the live write guard")
	}
	return g.inner.RoundTrip(req)
}

// snapshot is the human's lists as they are: every list that is not today's, with its lines.
func snapshot(t *testing.T, s *Store, today string) map[string]string {
	t.Helper()
	ctx := context.Background()
	uid, err := s.user()
	if err != nil {
		t.Fatal(err)
	}
	lists, err := pages[apiList](ctx, s, call{method: "GET", path: fmt.Sprintf("v3/users/%d/shopping-lists", uid), auth: true},
		"lister", 100, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, l := range lists {
		if l.Name == today {
			continue
		}
		full, err := s.readList(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		lines := make([]string, 0, len(full.Items))
		for _, it := range full.Items {
			lines = append(lines, fmt.Sprintf("%s:%s:%t", it.StoreItemID, it.Amount, it.Bought))
		}
		out[string(l.ID)] = fmt.Sprintf("%s|primary=%t|%s", l.Name, l.IsPrimary, strings.Join(lines, ","))
	}
	return out
}

func TestLiveWrite(t *testing.T) {
	if os.Getenv("HANDLE_LIVE") != "1" || os.Getenv("HANDLE_LIVE_WRITE") != "1" {
		t.Skip("set HANDLE_LIVE=1 and HANDLE_LIVE_WRITE=1 (the human's yes) to write to today's list")
	}
	ctx := context.Background()
	probe := New(nil)
	guard := &onlyToday{tb: t, inner: http.DefaultTransport, name: probe.listName()}
	s := New(&http.Client{Transport: guard})
	if _, err := s.ready(ctx); err != nil {
		t.Fatal(err)
	}
	today := s.listName()
	before := snapshot(t, s, today)

	list, err := s.findToday(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list != nil {
		guard.setID(string(list.ID))
	} else {
		// creating is allowed by the guard on the exact body; the id is set once it exists
		if _, err := s.createToday(ctx); err != nil {
			t.Fatal(err)
		}
		if list, err = s.findToday(ctx); err != nil || list == nil {
			t.Fatalf("today's list after creating: %v", err)
		}
		guard.setID(string(list.ID))
	}

	products, err := s.Search(ctx, "minimælk", 1)
	if err != nil || len(products) == 0 {
		t.Fatalf("search: %v", err)
	}
	id := products[0].ID
	orig := 0
	cart, err := s.GetCart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range cart.Lines {
		if l.ProductID == id {
			orig = l.Quantity
		}
	}
	t.Cleanup(func() { // put today's list back as it was
		if _, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: id, Quantity: orig}}); err != nil {
			t.Errorf("restore: %v", err)
		}
	})

	for _, q := range []int{1, 0} {
		res, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: id, Quantity: q}})
		if err != nil || len(res) != 1 || !res[0].OK || res[0].Applied == nil || *res[0].Applied != q {
			t.Fatalf("set %d: %+v, %v", q, res, err)
		}
		if res[0].Message != nil {
			t.Logf("message: %s", *res[0].Message)
		}
	}
	after := snapshot(t, s, today)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the human's lists changed:\nbefore %v\nafter  %v", before, after)
	}
}
