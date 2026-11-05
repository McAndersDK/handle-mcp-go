package nemlig

// A fake nemlig.com: a Go port of FakeNemlig and ScenarioNemlig (tests/test_nemlig.py and
// scripts/golden_fakes.py). It is an http.RoundTripper: no socket is ever opened, and every
// request is recorded so a test can check the method, URL, params, body, headers and cookies.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fixturePath is tests/fixtures/nemlig/<name>.json.
func fixturePath(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "tests", "fixtures", "nemlig", name+".json")
}

// fixture reads a fixture into plain values. Each call gives a fresh copy to change.
func fixture(tb testing.TB, name string) any {
	tb.Helper()
	raw, err := os.ReadFile(fixturePath(name))
	if err != nil {
		tb.Fatal(err)
	}
	v, err := decodeJSON(raw)
	if err != nil {
		tb.Fatalf("%s: %v", name, err)
	}
	return v
}

// resp is a scripted answer. Zero body and text mean an empty body.
type resp struct {
	status  int
	body    any
	text    string
	hasText bool
	headers map[string]string
}

func jsonResp(status int, body any) *resp { return &resp{status: status, body: body} }
func textResp(status int, text string) *resp {
	return &resp{status: status, text: text, hasText: true}
}
func emptyResp(status int) *resp { return &resp{status: status} }
func redirect(location string) *resp {
	return &resp{status: 302, headers: map[string]string{"Location": location}}
}

// record is one request as the golden files list it.
type record struct {
	Method  string
	URL     string // without the query
	Params  [][2]string
	Body    any
	Headers map[string]string // lower-case names, only the ones the goldens list
	Cookies []string          // names, sorted
}

var goldenHeaders = []string{
	"accept", "authorization", "content-type", "device-size", "platform", "referer", "user-agent",
	"version", "x-xsrf-token",
}

// queryPairs reads a raw query in order, which url.Values cannot.
func queryPairs(raw string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		uk, _ := url.QueryUnescape(k)
		uv, _ := url.QueryUnescape(v)
		out = append(out, [2]string{uk, uv})
	}
	return out
}

type fake struct {
	mu       sync.Mutex
	tb       testing.TB
	requests []record
	basket   obj
	scripted map[string][]any // path -> a status (int) or an error, given first
	respond  map[string][]any // path -> *resp or an error, given first
	tokens   int

	queue, apiQueue, endlessDays bool
	history, details             string
	onRequest                    func(*http.Request) // for tests that watch (called with mu held)
	rejectBearer                 map[string]bool     // a token nemlig has expired: its basket calls get 401
}

func newFake(tb testing.TB) *fake {
	f := &fake{tb: tb, scripted: map[string][]any{}, respond: map[string][]any{},
		history: "order_history", details: "order_details"}
	f.basket = fixture(tb, "basket").(obj)
	f.basket["TimeslotUtc"] = "2026110316-60-180"
	f.basket["DeliveryZoneId"] = json.Number("3")
	f.basket["DeliveryTimeSlot"] = obj{"Id": "2435672"} // live basket field, 2026-11-03
	return f
}

var searchFixtures = map[string]string{
	"broccoli": "match_broccoli", "hvidløg": "match_hvidloeg", "hakkede tomater": "match_hakkede_tomater",
	"hakkede tomater 30": "match_hakkede_tomater_30", "makrel i tomat": "match_makrel_tomat",
}

func (f *fake) record(r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	rec := record{Method: r.Method, Params: queryPairs(r.URL.RawQuery), Headers: map[string]string{}, Cookies: []string{}}
	u := *r.URL
	u.RawQuery, u.Fragment = "", ""
	rec.URL = u.String()
	if len(body) > 0 {
		if strings.Contains(r.Header.Get("Content-Type"), "json") {
			v, err := decodeJSON(body)
			if err != nil {
				f.tb.Errorf("request body is not JSON: %v", err)
			}
			rec.Body = v
		} else {
			rec.Body = string(body)
		}
	}
	lower := map[string]string{}
	for k, v := range r.Header {
		lower[strings.ToLower(k)] = strings.Join(v, ",")
	}
	for _, k := range goldenHeaders {
		if v, ok := lower[k]; ok {
			rec.Headers[k] = v
		}
	}
	for _, c := range r.Cookies() {
		rec.Cookies = append(rec.Cookies, c.Name)
	}
	sort.Strings(rec.Cookies)
	f.requests = append(f.requests, rec)
	if f.onRequest != nil {
		f.onRequest(r)
	}
}

func (f *fake) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	for i, r := range f.requests {
		u, _ := url.Parse(r.URL)
		out[i] = u.Path
	}
	return out
}

func (f *fake) count(path string) int {
	n := 0
	for _, p := range f.paths() {
		if p == path {
			n++
		}
	}
	return n
}

func (f *fake) last(path string) record {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if strings.HasSuffix(f.requests[i].URL, path) {
			return f.requests[i]
		}
	}
	f.tb.Fatalf("no request to %s", path)
	return record{}
}

func (r record) param(key string) (string, bool) {
	for _, p := range r.Params {
		if p[0] == key {
			return p[1], true
		}
	}
	return "", false
}

func build(req *http.Request, r *resp) *http.Response {
	h := http.Header{}
	var body []byte
	switch {
	case r.hasText:
		body = []byte(r.text)
	case r.body != nil:
		var err error
		if body, err = json.Marshal(r.body); err != nil {
			panic(err)
		}
		h.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: r.status, Status: strconv.Itoa(r.status), Header: h,
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

// RoundTrip is ScenarioNemlig.handle.
func (f *fake) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil { // a real transport stops for a cancelled call
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := req.URL.Path
	if q := f.respond[path]; len(q) > 0 {
		f.record(req)
		next := q[0]
		f.respond[path] = q[1:]
		if err, ok := next.(error); ok {
			return nil, err
		}
		return build(req, next.(*resp)), nil
	}
	if path == "/searchgateway/api/search" {
		if name, ok := searchFixtures[req.URL.Query().Get("query")]; ok {
			f.record(req)
			return build(req, jsonResp(200, fixture(f.tb, name))), nil
		}
	}
	return f.handle(req)
}

func (f *fake) handle(req *http.Request) (*http.Response, error) {
	f.record(req)
	path, q := req.URL.Path, req.URL.Query()
	if f.rejectBearer[req.Header.Get("Authorization")] && strings.HasPrefix(path, "/webapi/basket/") {
		return build(req, emptyResp(401)), nil
	}
	if s := f.scripted[path]; len(s) > 0 {
		next := s[0]
		f.scripted[path] = s[1:]
		if err, ok := next.(error); ok {
			return nil, err
		}
		return build(req, emptyResp(next.(int))), nil
	}
	if req.URL.Hostname() == "nemlig.queue-it.net" {
		return build(req, textResp(200, "<html>Du er i kø</html>")), nil
	}
	if (path == "/" || path == "/tilbud") && f.queue { // page URLs, never /webapi/
		return build(req, redirect("https://nemlig.queue-it.net/?c=nemlig")), nil
	}
	ok := func(name string) (*http.Response, error) { return build(req, jsonResp(200, fixture(f.tb, name))), nil }
	switch {
	case path == "/webapi/AntiForgery":
		return build(req, jsonResp(200, obj{"Header": "X-XSRF-TOKEN", "Value": "xsrf-1"})), nil
	case path == "/webapi/Token":
		f.tokens++
		return build(req, jsonResp(200, obj{"access_token": fmt.Sprintf("bearer-%d", f.tokens), "expires_in": json.Number("300")})), nil
	case path == "/webapi/login":
		raw, _ := io.ReadAll(req.Body)
		v, _ := decodeJSON(raw)
		if v.(obj)["Password"] != "hemmeligt" {
			return build(req, emptyResp(401)), nil
		}
		r := jsonResp(200, obj{"RedirectUrl": "/", "TimeslotUtc": "x"})
		r.headers = map[string]string{"Set-Cookie": ".ASPXAUTH=auth-1; Path=/; HttpOnly"}
		return build(req, r), nil
	case path == "/webapi/v2/AppSettings/Website":
		return build(req, jsonResp(200, obj{"CombinedProductsAndSitecoreTimestamp": "ts-1"})), nil
	case strings.HasPrefix(path, "/productbff/") && f.apiQueue:
		return build(req, redirect("https://nemlig.queue-it.net/")), nil
	case strings.HasPrefix(path, "/productbff/api/web/meals/recipeDetails/"):
		return ok("recipe_details")
	case path == "/productbff/api/web/meals/recipeProductDialog":
		return ok("recipe_products")
	case path == "/productbff/api/web/search/recipes":
		return ok("recipe_search")
	case path == "/productbff/api/web/meals/allRecipes":
		return ok("recipes_by_tag")
	case path == "/productbff/api/web/page":
		page := map[string]string{"/maaltider": "meals_page", "/tilbud": "offers_page"}[q.Get("path")]
		if page == "" {
			f.tb.Errorf("unknown page %q", q.Get("path"))
		}
		return ok(page)
	case path == "/searchgateway/api/search":
		switch q.Get("query") {
		case "mums rosinboller", "gratis":
			return ok("search_gift")
		case "kyllingebryst":
			hits := fixture(f.tb, "search_kyllingebryst").(obj) // live 2026-11-03: the cheapest is hit 30
			take, _ := strconv.Atoi(q.Get("take"))
			inner := hits["Products"].(obj)
			inner["Products"] = sliceTo(inner["Products"].([]any), take)
			return build(req, jsonResp(200, hits)), nil
		}
		return ok("search")
	case path == "/webapi/basket/GetBasket":
		if !hasAuthCookie(req) {
			anon := obj{}
			for k, v := range f.basket {
				anon[k] = v
			}
			anon["Email"] = nil
			return build(req, jsonResp(200, anon)), nil
		}
		return build(req, jsonResp(200, f.basket)), nil
	case path == "/webapi/basket/AddToBasket":
		raw, _ := io.ReadAll(req.Body)
		v, _ := decodeJSON(raw)
		body := v.(obj)
		pid, qty := body["ProductId"].(string), body["quantity"].(json.Number)
		if pid == "sold-out" {
			qty = json.Number("0") // nemlig quietly refuses
		}
		lines := []any{}
		for _, l := range f.basket["Lines"].([]any) {
			if l.(obj)["Id"] != pid {
				lines = append(lines, l)
			}
		}
		if n, _ := qty.Int64(); n != 0 {
			lines = append(lines, obj{"Id": pid, "Name": "Vare", "Quantity": qty, "Price": json.Number(fmt.Sprintf("%.1f", 10.0*float64(n)))})
		}
		next := obj{}
		for k, v := range f.basket {
			next[k] = v
		}
		next["Lines"] = lines
		f.basket = next
		return build(req, jsonResp(200, f.basket)), nil
	case path == "/webapi/v2/Delivery/GetDeliveryDays":
		if q.Get("startDate") != "" && !f.endlessDays { // no more days
			return build(req, jsonResp(200, obj{"DayRangeHours": []any{}, "NextRangeStart": nil})), nil
		}
		return ok("delivery_days")
	case path == "/webapi/Order/DeliverySpot":
		return ok("delivery_spot")
	case path == "/webapi/order/GetBasicOrderHistory":
		return ok(f.history)
	case strings.HasPrefix(path, "/webapi/v2/order/GetOrderHistory/"):
		return ok(f.details)
	}
	return build(req, emptyResp(404)), nil
}

func hasAuthCookie(req *http.Request) bool {
	for _, c := range req.Cookies() {
		if c.Name == authCookie && c.Value == "auth-1" {
			return true
		}
	}
	return false
}

// netErr is a transport error with the shape of the ones httpx raised.
type netErr struct {
	text    string
	timeout bool
}

func (e *netErr) Error() string   { return e.text }
func (e *netErr) Timeout() bool   { return e.timeout }
func (e *netErr) Temporary() bool { return e.timeout }

var (
	errReadError   = &netErr{text: "connection reset"}                       // httpx.ReadError
	errReadTimeout = &netErr{text: "read timed out", timeout: true}          // httpx.ReadTimeout
	errUnsupported = errors.New("unsupported protocol: not a network error") // httpx.UnsupportedProtocol
)
