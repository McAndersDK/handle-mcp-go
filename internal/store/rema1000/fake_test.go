package rema1000

// A fake REMA 1000 for the tests: a transport that answers from the fixtures in
// tests/fixtures/rema1000 and keeps a small, real shopping-list state, so a write can be read back.
// It records every request. No test reaches the network or a real keychain.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("HANDLE_LIVE") != "1" {
		keyring.MockInit() // no test may reach the real keychain; every test also calls isolate
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, dk)

const (
	userID  = 7777
	storeID = 9100
)

type obj = map[string]any

func fixture(tb testing.TB, file string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "rema1000", file))
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

func fixtureJSON(tb testing.TB, file string) obj {
	tb.Helper()
	var o obj
	if err := json.Unmarshal(fixture(tb, file), &o); err != nil {
		tb.Fatal(err)
	}
	return o
}

// makeJWT builds an unsigned JWT with the claims the adapter reads.
func makeJWT(exp time.Time, sub int) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(obj{"alg": "none"}) + "." + enc(obj{"exp": exp.Unix(), "sub": strconv.Itoa(sub)}) + ".sig"
}

type request struct{ Method, Host, Path, Query, Body, Auth, Cookie string }

// apiValue is the api_session cookie of the fake's session.
const apiValue = "api-1"

// fakeItem and fakeList are the lists the fake keeps.
type fakeItem struct {
	ID, StoreItemID, Amount int
	Bought                  bool
}
type fakeList struct {
	ID      int
	Name    string
	Primary bool
	Items   []fakeItem
}

// fake is the whole fake REMA 1000.
type fake struct {
	tb testing.TB
	mu sync.Mutex

	log               []request
	lists             []*fakeList
	nextID            int
	storeActive       bool
	others            []fakeStore // more stores in the list, after the main one (store_test.go)
	createPrimary     bool        // a list created now is primary
	refusePOST        string      // a product id that POST items answers 422 for
	access            string      // the access token the API accepts
	refreshCookie     string      // the refresh cookie the site holds; "" = refresh refuses
	nextAccess        string      // what a refresh hands out
	rotate            string      // a new refresh cookie a refresh sets
	rotateAPI         string      // a new api_session cookie a refresh sets
	loginOK           bool
	queueOnLogin      bool
	leak              bool              // add personal data to every JSON object (canary_test.go)
	listDelay         time.Duration     // the list of lists answers this slowly (concurrent_test.go)
	lose              map[string]string // "POST list" or "POST items": the change is made, then the answer is "500" or "net"
	loseWithoutEffect bool              // a lost answer means the change was not made
}

func newFake(tb testing.TB) *fake {
	f := &fake{tb: tb, nextID: 100, storeActive: true, loginOK: true}
	f.access = makeJWT(testNow.Add(time.Hour), userID)
	f.nextAccess = makeJWT(testNow.Add(2*time.Hour), userID)
	f.refreshCookie = "refresh-1"
	return f
}

func (f *fake) count(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.log {
		if r.Method == method && strings.Contains(r.Path, path) {
			n++
		}
	}
	return n
}

func (f *fake) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.log...)
}

func (f *fake) addList(name string, primary bool, items ...fakeItem) *fakeList {
	l := &fakeList{ID: f.nextID, Name: name, Primary: primary, Items: items}
	f.nextID++
	f.lists = append(f.lists, l)
	return l
}

type reply struct {
	status int
	body   any
	header http.Header
}

func (f *fake) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}
	if f.listDelay > 0 && req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/shopping-lists") {
		time.Sleep(f.listDelay)
	}
	f.mu.Lock()
	f.log = append(f.log, request{req.Method, req.URL.Host, req.URL.Path, req.URL.RawQuery, string(body), req.Header.Get("Authorization"), req.Header.Get("Cookie")})
	r := reply{}
	kind := ""
	switch {
	case req.Method == "POST" && req.URL.Path == "/api/v2/shoppinglists":
		kind = "POST list"
	case req.Method == "POST" && itemsRe.MatchString(req.URL.Path):
		kind = "POST items"
	}
	lost := f.lose[kind]
	delete(f.lose, kind) // once
	if lost == "" || !f.loseWithoutEffect {
		r = f.route(req, body)
	}
	f.mu.Unlock()
	switch lost {
	case "500":
		r = reply{status: 500}
	case "net":
		return nil, timeoutErr{}
	}
	var raw []byte
	switch b := r.body.(type) {
	case nil:
	case []byte:
		raw = b
	default:
		if f.leak {
			b = leakAll(b)
		}
		raw, _ = json.Marshal(b)
	}
	h := r.header
	if h == nil {
		h = http.Header{}
	}
	if r.status == 0 {
		r.status = 200
	}
	return &http.Response{StatusCode: r.status, Header: h, Body: io.NopCloser(bytes.NewReader(raw)), Request: req}, nil
}

var (
	timeSlotsRe = regexp.MustCompile(`^/api/v3/stores/\d+/time-slots$`)
	listPath    = regexp.MustCompile(`^/api/v2/shoppinglists/(\d+)$`)
	itemsRe     = regexp.MustCompile(`^/api/v2/shoppinglists/(\d+)/items$`)
	itemRe      = regexp.MustCompile(`^/api/v2/shoppinglists/(\d+)/items/(\d+)$`)
)

func (f *fake) route(req *http.Request, body []byte) reply {
	host, path, q := req.URL.Host, req.URL.Path, req.URL.Query()
	if page := q.Get("page"); page != "" && page != "1" && (strings.Contains(path, "/search/") || strings.Contains(path, "/entities")) {
		return reply{body: obj{"data": []any{}}} // the fixtures are page 1 only
	}
	auth := req.Header.Get("Authorization")
	needAuth := func() *reply {
		if auth != "Bearer "+f.access {
			return &reply{status: 401}
		}
		return nil
	}
	switch {
	case host == "shop.rema1000.dk" && path == "/konto/auth":
		return reply{status: 302, header: http.Header{"Location": {"https://login.rema1000.dk/login?x=1"}}}
	case host == "login.rema1000.dk":
		return reply{body: []byte("<html>login</html>")}
	case host == "cphapp.rema1000.dk" && path == "/api/oauth2/login":
		switch {
		case f.queueOnLogin:
			return reply{status: 200, body: obj{"redirect_uri": "https://rema.queue-it.net/?c=1"}}
		case !f.loginOK:
			return reply{status: 401}
		}
		return reply{body: obj{"redirect_uri": "http://api.digital.rema1000.dk/oauth/authorize?code=1"}}
	case host == "api.digital.rema1000.dk" && path == "/oauth/authorize":
		return reply{status: 302, header: http.Header{"Location": {"https://shop.rema1000.dk/api/auth/callback/rema?code=1"}}}
	case host == "shop.rema1000.dk" && path == callbackPath:
		h := http.Header{}
		h.Add("Set-Cookie", "access_token="+f.access+"; Path=/")
		h.Add("Set-Cookie", "refresh_token=refresh-login; Path=/")
		h.Add("Set-Cookie", "api_session=api-login; Path=/")
		h.Set("Location", "https://shop.rema1000.dk/konto")
		return reply{status: 302, header: h}
	case host == "shop.rema1000.dk" && path == "/api/auth/refresh":
		// As the live site: 500 without api_session, otherwise 200 with an empty body and the
		// cookies set again.
		if c, _ := req.Cookie(apiCookie); c == nil {
			return reply{status: 500}
		}
		cookie, _ := req.Cookie(refreshCookie)
		if f.refreshCookie == "" || cookie == nil || cookie.Value != f.refreshCookie {
			return reply{status: 401}
		}
		f.access = f.nextAccess
		h := http.Header{}
		h.Add("Set-Cookie", "access_token="+f.access+"; Path=/")
		if f.rotate != "" {
			f.refreshCookie = f.rotate
			h.Add("Set-Cookie", "refresh_token="+f.rotate+"; Path=/")
		}
		if f.rotateAPI != "" {
			h.Add("Set-Cookie", "api_session="+f.rotateAPI+"; Path=/")
		}
		return reply{header: h}
	case host != "api.digital.rema1000.dk":
		f.tb.Errorf("request to an unexpected host: %s%s", host, path)
		return reply{status: 599}
	}

	switch {
	case path == "/api/search/products":
		if q.Get("filter[labels]") == advertisedLabel {
			return reply{body: fixtureJSON(f.tb, "offers.json")}
		}
		return reply{body: fixtureJSON(f.tb, "search_milk.json")}
	case path == "/api/search/recipes":
		return reply{body: fixtureJSON(f.tb, "recipes_search.json")}
	case path == "/api/rema1000dk/entities":
		return f.entities(q)
	}
	if r := needAuth(); r != nil {
		return *r
	}
	switch {
	case path == "/api/v3/stores":
		return reply{body: f.stores()}
	case timeSlotsRe.MatchString(path):
		return reply{body: fixtureJSON(f.tb, "time_slots.json")}
	case path == fmt.Sprintf("/api/v3/users/%d/shopping-lists", userID):
		var data []obj
		for _, l := range f.lists {
			data = append(data, obj{"id": l.ID, "name": l.Name, "is_primary": l.Primary})
		}
		return reply{body: obj{"data": data, "meta": obj{"pagination": obj{"last_page": 1}}}}
	case path == "/api/v2/shoppinglists" && req.Method == "POST":
		var in struct {
			Name    string `json:"name"`
			Primary bool   `json:"primary"`
		}
		_ = json.Unmarshal(body, &in)
		l := f.addList(in.Name, in.Primary || f.createPrimary)
		return reply{status: 201, body: obj{"id": l.ID, "name": l.Name}}
	}
	return f.listRoutes(req, body)
}

func (f *fake) find(id string) *fakeList {
	n, _ := strconv.Atoi(id)
	for _, l := range f.lists {
		if l.ID == n {
			return l
		}
	}
	return nil
}

func (f *fake) listRoutes(req *http.Request, body []byte) reply {
	path := req.URL.Path
	if m := listPath.FindStringSubmatch(path); m != nil {
		l := f.find(m[1])
		if l == nil {
			return reply{status: 404}
		}
		if req.Method == "GET" {
			return reply{body: f.listJSON(l)}
		}
		var in struct {
			Items []struct {
				ID     int  `json:"id"`
				Delete bool `json:"delete"`
			} `json:"items"`
		}
		_ = json.Unmarshal(body, &in)
		for _, d := range in.Items {
			for i, it := range l.Items {
				if d.Delete && it.ID == d.ID {
					l.Items = append(l.Items[:i], l.Items[i+1:]...)
					break
				}
			}
		}
		return reply{body: f.listJSON(l)}
	}
	if m := itemsRe.FindStringSubmatch(path); m != nil && req.Method == "POST" {
		l := f.find(m[1])
		var in struct{ Amount, StoreID, StoreItemID int }
		var raw map[string]int
		_ = json.Unmarshal(body, &raw)
		in.Amount, in.StoreID, in.StoreItemID = raw["amount"], raw["store_id"], raw["store_item_id"]
		if l == nil || strconv.Itoa(in.StoreItemID) == f.refusePOST || in.StoreID != 1 {
			return reply{status: 422, body: obj{"errors": obj{"store_item_id": []string{"invalid"}}}}
		}
		l.Items = append(l.Items, fakeItem{ID: f.nextID, StoreItemID: in.StoreItemID, Amount: in.Amount})
		f.nextID++
		return reply{status: 201, body: obj{}}
	}
	if m := itemRe.FindStringSubmatch(path); m != nil && req.Method == "PATCH" {
		l := f.find(m[1])
		var in struct {
			Amount int   `json:"amount"`
			Bought *bool `json:"bought"`
		}
		_ = json.Unmarshal(body, &in)
		n, _ := strconv.Atoi(m[2])
		for i := range l.Items {
			if l.Items[i].ID == n {
				l.Items[i].Amount = in.Amount
				if in.Bought != nil {
					l.Items[i].Bought = *in.Bought
				}
				return reply{body: obj{}}
			}
		}
		return reply{status: 404}
	}
	f.tb.Errorf("unexpected request: %s %s", req.Method, path)
	return reply{status: 404}
}

// listJSON is a list as GET v2/shoppinglists/{id}?with=items answers, with the items from the
// fixture (so the real shape is read) and the fake's own ids and amounts.
func (f *fake) listJSON(l *fakeList) obj {
	items := []obj{}
	for _, it := range l.Items {
		item := fixtureJSON(f.tb, "list_items.json")["items"].([]any)[0].(obj)
		item["id"], item["amount"], item["bought"] = it.ID, it.Amount, it.Bought
		item["store_item_id"] = it.StoreItemID
		si := item["store_item"].(obj)
		si["id"], si["name"] = it.StoreItemID, "VARE "+strconv.Itoa(it.StoreItemID)
		items = append(items, item)
	}
	return obj{"id": l.ID, "name": l.Name, "primary": l.Primary, "items": items, "total": len(items)}
}

// stores is the store list: the fixture's first store, made active or not, with a phone number
// and address that must never come out.
func (f *fake) stores() obj {
	o := fixtureJSON(f.tb, "stores.json")
	data := o["data"].([]any)
	s := data[0].(obj)
	s["id"], s["is_click_and_collect_active"] = storeID, f.storeActive
	s["phone"] = "+45 98 76 54 32"
	list := []any{s}
	for _, o := range f.others {
		x := fixtureJSON(f.tb, "stores.json")["data"].([]any)[0].(obj)
		x["id"], x["name"], x["city"], x["postal_code"] = o.ID, o.Name, o.City, o.Post
		x["is_click_and_collect_active"] = o.Active
		x["phone"] = "+45 98 76 54 32"
		list = append(list, x)
	}
	return obj{"data": list, "meta": obj{"pagination": obj{"last_page": 1}}}
}

func (f *fake) entities(q url.Values) reply {
	switch q.Get("filter[type]") {
	case "recipeTag":
		return reply{body: fixtureJSON(f.tb, "recipe_tags.json")}
	}
	if slug := q.Get("filter[slug]"); slug != "" {
		o := fixtureJSON(f.tb, "recipe_by_slug.json")
		if o["data"].([]any)[0].(obj)["fields"].(obj)["slug"] != slug {
			o["data"] = []any{}
		}
		return reply{body: o}
	}
	return reply{body: fixtureJSON(f.tb, "recipes_listing.json")}
}

// isolate gives a test a clean world: no HANDLE_ variable, an empty config folder and keychain.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "HANDLE_") {
			old := os.Getenv(key)
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, old) })
		}
	}
	t.Setenv("HANDLE_CONFIG_DIR", t.TempDir())
}

// saveSession puts a saved session in the (mock) keychain.
func saveSession(t *testing.T, session map[string]string) {
	t.Helper()
	secret, _ := json.Marshal(obj{"email": "kunde@example.com", "session": session})
	if err := keyring.Set(store.KeychainService, name, string(secret)); err != nil {
		t.Fatal(err)
	}
}

func savedSession(t *testing.T) map[string]string {
	t.Helper()
	secret, err := keyring.Get(store.KeychainService, name)
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Session map[string]string }
	if err := json.Unmarshal([]byte(secret), &out); err != nil {
		t.Fatal(err)
	}
	return out.Session
}

// harness is a store on the fake, with a valid saved session and the pickup store chosen.
func harness(t *testing.T) (*fake, *Store) {
	t.Helper()
	isolate(t)
	f := newFake(t)
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, apiCookie: apiValue, "store_id": strconv.Itoa(storeID)})
	return f, newStore(f)
}

func newStore(f *fake) *Store {
	return New(&http.Client{Transport: f},
		WithClock(func() time.Time { return testNow }),
		WithSleep(func(context.Context, time.Duration) error { return nil }))
}

// fakeStore is one more store in the fake's list.
type fakeStore struct {
	ID               int
	Name, City, Post string
	Active           bool
}
