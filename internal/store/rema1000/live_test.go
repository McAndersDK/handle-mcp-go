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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
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

// skipIfGateClosed skips the test when the first REMA call hit a closed gate (no chosen store has
// Køb & Hent yet, or none is left). Any other error is left for the caller to fail on.
func skipIfGateClosed(t *testing.T, err error) {
	t.Helper()
	if err != nil && gateClosed(err) {
		t.Skipf("%v (add a Køb & Hent store at handle-mcp login rema1000 to test this)", err)
	}
}

func TestLiveSearchAndOffers(t *testing.T) {
	s := liveStore(t)
	products, err := s.Search(context.Background(), "minimælk", 5)
	skipIfGateClosed(t, err)
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
	skipIfGateClosed(t, err)
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
	skipIfGateClosed(t, err)
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

// checkWrite is the live write guard's rule for one request that is not a GET. It allows only:
//   - creating today's list: POST v2/shoppinglists with exactly the create body (the one place
//     "name" and "primary" may appear, and primary is false);
//   - on the list whose id is given: a list PATCH with only "items", each with only "id" and
//     delete:true; an item PATCH with only "amount" and bought:false; an item POST with only
//     "amount", "store_id" and "store_item_id".
//
// It returns nil for an allowed request and a reason otherwise.
func checkWrite(method, path string, body []byte, name, id string) error {
	m := writePath.FindStringSubmatch(path)
	if m == nil {
		return errors.New("not a shopping-list path")
	}
	if m[1] == "" {
		if method == http.MethodPost && string(body) == fmt.Sprintf(`{"name":%q,"primary":false,"items":[]}`, name) {
			return nil
		}
		return errors.New("only today's list may be created, and only with the exact create body")
	}
	if id == "" || m[1] != id {
		return errors.New("not today's list")
	}
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		return errors.New("the body is not a JSON object")
	}
	onlyKeys := func(allowed ...string) error {
		for k := range in {
			if !slices.Contains(allowed, k) {
				return fmt.Errorf("the field %q is not allowed here", k)
			}
		}
		return nil
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := in[k]; !ok {
				return false
			}
		}
		return true
	}
	itemPath := strings.Contains(path, "/items/") // one line; "/items" alone is the collection
	switch {
	case method == http.MethodPatch && !itemPath && !strings.HasSuffix(path, "/items"): // remove lines
		items, ok := in["items"].([]any)
		if err := onlyKeys("items"); err != nil || !ok || len(items) == 0 {
			return errors.New("a list PATCH may only hold items to delete")
		}
		for _, raw := range items {
			it, ok := raw.(map[string]any)
			if !ok || len(it) != 2 || it["id"] == nil || it["delete"] != true {
				return errors.New("a list PATCH item may only be {id, delete:true}")
			}
		}
		return nil
	case method == http.MethodPatch && itemPath: // change one line
		if err := onlyKeys("amount", "bought"); err != nil || !has("amount") {
			return errors.New("an item PATCH may only hold amount and bought:false")
		}
		if b, there := in["bought"]; there && b != false {
			return errors.New("bought may only be false")
		}
		return nil
	case method == http.MethodPost && strings.HasSuffix(path, "/items"):
		if err := onlyKeys("amount", "store_id", "store_item_id"); err != nil || !has("amount", "store_id", "store_item_id") {
			return errors.New("an item POST may only hold amount, store_id and store_item_id")
		}
		return nil
	}
	return errors.New("this method is not allowed here")
}

// onlyToday allows writes to one place (see checkWrite). Everything else that is not a GET, the
// login or the token renewal is refused.
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
	if err := checkWrite(req.Method, req.URL.Path, body, g.name, id); err != nil {
		g.tb.Errorf("a write the guard refuses: %s %s %s: %v", req.Method, req.URL.Path, body, err)
		return nil, errors.New("refused by the live write guard")
	}
	return g.inner.RoundTrip(req)
}

func TestLiveWriteGuardChecksBodies(t *testing.T) {
	const name, id = "handle-mcp 2026-11-07", "42"
	base := "/api/v2/shoppinglists"
	create := fmt.Sprintf(`{"name":%q,"primary":false,"items":[]}`, name)
	for _, c := range []struct {
		method, path, body string
		ok                 bool
	}{
		{"POST", base, create, true},
		{"POST", base, `{"name":"x","primary":false,"items":[]}`, false},
		{"POST", base, `{"name":"` + name + `","primary":true,"items":[]}`, false},
		{"POST", base + "/42/items", `{"amount":1,"store_id":1,"store_item_id":5}`, true},
		{"POST", base + "/42/items", `{"amount":1,"store_id":1,"store_item_id":5,"name":"x"}`, false},
		{"POST", base + "/42/items", `{"amount":1,"store_id":1}`, false},
		{"POST", base + "/43/items", `{"amount":1,"store_id":1,"store_item_id":5}`, false},
		{"PATCH", base + "/42/items/9", `{"amount":2}`, true},
		{"PATCH", base + "/42/items/9", `{"amount":2,"bought":false}`, true},
		{"PATCH", base + "/42/items/9", `{"amount":2,"bought":true}`, false},
		{"PATCH", base + "/42/items/9", `{"amount":2,"name":"x"}`, false},
		{"PATCH", base + "/42/items/9", `{"amount":2,"primary":true}`, false},
		{"PATCH", base + "/42", `{"items":[{"id":9,"delete":true}]}`, true},
		{"PATCH", base + "/42", `{"items":[{"id":9,"delete":false}]}`, false},
		{"PATCH", base + "/42", `{"items":[{"id":9,"delete":true,"amount":1}]}`, false},
		{"PATCH", base + "/42", `{"name":"x","items":[{"id":9,"delete":true}]}`, false},
		{"PATCH", base + "/42", `{"primary":true}`, false},
		{"PATCH", base + "/42", `{"items":[]}`, false},
		{"PATCH", base + "/43", `{"items":[{"id":9,"delete":true}]}`, false},
		{"DELETE", base + "/42", ``, false},
		{"POST", "/api/v3/cart", `{}`, false},
	} {
		err := checkWrite(c.method, c.path, []byte(c.body), name, id)
		if c.ok != (err == nil) {
			t.Errorf("%s %s %s: ok=%v err=%v", c.method, c.path, c.body, c.ok, err)
		}
	}
}

// rawLines is one list as it is, bought lines included, one string per line, sorted.
func rawLines(items []apiItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s:%s:%s:%t", it.ID, it.StoreItemID, it.Amount, it.Bought))
	}
	slices.Sort(out)
	return out
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
		out[string(l.ID)] = fmt.Sprintf("%s|primary=%t|%s", l.Name, l.IsPrimary, strings.Join(rawLines(full.Items), ","))
	}
	return out
}

// todayRaw is today's list id and its raw lines (bought included), or "" and nil when there is none.
func todayRaw(t *testing.T, s *Store) (string, []string) {
	t.Helper()
	list, err := s.findToday(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list == nil {
		return "", nil
	}
	full, err := s.readList(context.Background(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	return string(list.ID), rawLines(full.Items)
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
		skipIfGateClosed(t, err)
		t.Fatal(err)
	}
	today := s.listName()
	before := snapshot(t, s, today)
	listBefore, linesBefore := todayRaw(t, s)

	// A probe product that is not on today's list at all, bought or not, so the cleanup can never
	// change a line the human bought.
	onList := map[string]bool{}
	if listBefore != "" {
		full, err := s.readList(ctx, json.Number(listBefore))
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range full.Items {
			onList[string(it.StoreItemID)] = true
		}
	}
	id := ""
	for _, term := range []string{"minimælk", "smør", "havregryn", "pasta"} {
		found, err := s.Search(ctx, term, 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		for _, p := range found {
			if id == "" && !onList[p.ID] {
				id = p.ID
			}
		}
	}
	if id == "" {
		t.Skip("every candidate product is already on today's list; nothing safe to probe with")
	}

	// The restore runs first, then the check that today's list is as it was.
	defer func() {
		if _, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: id, Quantity: 0}}); err != nil {
			t.Errorf("restore: %v", err)
		}
		listAfter, linesAfter := todayRaw(t, s)
		if listBefore != "" && listAfter != listBefore {
			t.Errorf("today's list changed id: %s -> %s", listBefore, listAfter)
		}
		if !slices.Equal(linesBefore, linesAfter) {
			t.Errorf("today's list is not as it was:\nbefore %v\nafter  %v", linesBefore, linesAfter)
		}
		if after := snapshot(t, s, today); !reflect.DeepEqual(before, after) {
			t.Errorf("the human's lists changed:\nbefore %v\nafter  %v", before, after)
		}
	}()

	if listBefore != "" {
		guard.setID(listBefore)
	} else {
		// creating is allowed by the guard on the exact body; the id is set once it exists
		list, err := s.createToday(ctx)
		if err != nil {
			t.Fatal(err)
		}
		guard.setID(string(list.ID))
	}
	for _, q := range []int{1, 0} {
		res, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: id, Quantity: q}})
		if err != nil || len(res) != 1 || !res[0].OK || res[0].Applied == nil || *res[0].Applied != q {
			t.Fatalf("set %d: %+v, %v", q, res, err)
		}
		if res[0].Message != nil {
			t.Logf("message: %s", *res[0].Message)
		}
	}
}
