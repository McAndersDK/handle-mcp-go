package bilkatogo

// A fake BilkaToGo: a Go port of FakeBilka and ScenarioBilka (tests/test_bilkatogo.py and
// scripts/golden_fakes.py). It answers like Gigya, api.bilkatogo.dk and Algolia. It is an
// http.RoundTripper: no socket is ever opened, and every request is recorded so a test can check
// the method, URL, params, body, headers and cookies.

import (
	"bytes"
	"encoding/json"
	"errors"
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

func fixturePath(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "tests", "fixtures", "bilkatogo", name+".json")
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

// record is one request as the golden files list it.
type record struct {
	Method  string
	URL     string // without the query
	Params  [][2]string
	Body    any
	Headers map[string]string // lower-case names, only the ones the goldens list
	Cookies []string          // names, sorted
}

var goldenHeaders = []string{"authorization", "content-type", "jwt_token", "x-algolia-application-id", "x-algolia-api-key"}

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
	mu        sync.Mutex
	tb        testing.TB
	requests  []record
	cart      obj
	scripted  map[string][]any // path -> a status (int), an error or a JSON body, given first
	respond   map[string][]any // path -> *resp or an error, given first
	onRequest func(*http.Request)
	setCookie string // what LoginJWT sets; default "cart=abc; Path=/"
	numbered  bool   // every getJWT answers with a new JWT: jwt-1, jwt-2 ...
	jwts      int
	rejectJWT map[string]bool // a JWT Bilka has expired: shop calls with it get 401
}

func newFake(tb testing.TB) *fake {
	return &fake{tb: tb, scripted: map[string][]any{}, respond: map[string][]any{},
		cart: fixture(tb, "cart_home").(obj)}
}

var searchFixtures = map[string]string{
	"fior di latte": "match_fior_di_latte", "mozzarella": "match_mozzarella", "hvidløg": "match_hvidloeg",
	"makrel i tomat": "match_makrel_tomat", "hakkede tomater": "match_hakkede_tomater",
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
		ct := r.Header.Get("Content-Type")
		switch {
		case strings.Contains(ct, "json"):
			v, err := decodeJSON(body)
			if err != nil {
				f.tb.Errorf("request body is not JSON: %v", err)
			}
			rec.Body = v
		case strings.Contains(ct, "x-www-form-urlencoded"):
			var pairs []any
			for _, kv := range queryPairs(string(body)) {
				pairs = append(pairs, []any{kv[0], kv[1]})
			}
			rec.Body = pairs
		default:
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

// all returns a copy of the requests to path, in order.
func (f *fake) all(path string) []record {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []record
	for _, r := range f.requests {
		if u, _ := url.Parse(r.URL); u.Path == path {
			out = append(out, r)
		}
	}
	return out
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

// RoundTrip is ScenarioBilka.handle.
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
	if path == "/1/indexes/*/queries" {
		body := f.peekBody(req)
		params := f.searchParams(body)
		if name, ok := searchFixtures[params.Get("query")]; ok && !strings.Contains(params.Get("filters"), "isInOffer") {
			f.record(req)
			return build(req, jsonResp(200, obj{"results": []any{fixture(f.tb, name)}})), nil
		}
	}
	return f.handle(req)
}

// peekBody reads the body and puts it back.
func (f *fake) peekBody(req *http.Request) []byte {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func (f *fake) searchParams(body []byte) url.Values {
	v, err := decodeJSON(body)
	if err != nil {
		f.tb.Errorf("Algolia body is not JSON: %v", err)
		return url.Values{}
	}
	text, _ := v.(obj)["requests"].([]any)[0].(obj)["params"].(string)
	q, _ := url.ParseQuery(text)
	return q
}

func (f *fake) handle(req *http.Request) (*http.Response, error) {
	f.record(req)
	path := req.URL.Path
	if f.rejectJWT[strings.Join(req.Header["jwt_token"], ",")] && strings.HasPrefix(path, "/api/shop/") {
		return build(req, emptyResp(401)), nil
	}
	if s := f.scripted[path]; len(s) > 0 {
		next := s[0]
		f.scripted[path] = s[1:]
		switch x := next.(type) {
		case error:
			return nil, x
		case int:
			return build(req, emptyResp(x)), nil
		}
		return build(req, jsonResp(200, next)), nil
	}
	ok := func(name string) (*http.Response, error) { return build(req, jsonResp(200, fixture(f.tb, name))), nil }
	switch path {
	case "/accounts.login":
		form, _ := url.ParseQuery(string(f.peekBody(req)))
		if form.Get("password") != "hemmeligt" {
			return build(req, jsonResp(403, fixture(f.tb, "gigya_login_invalid"))), nil
		}
		return ok("gigya_login")
	case "/accounts.getJWT":
		form, _ := url.ParseQuery(string(f.peekBody(req)))
		if form.Get("login_token") != "st2.fake-login-token" {
			invalid := fixture(f.tb, "gigya_login_invalid").(obj)
			invalid["errorCode"] = json.Number("403005")
			return build(req, jsonResp(200, invalid)), nil
		}
		if f.numbered {
			f.jwts++
			answer := fixture(f.tb, "gigya_jwt").(obj)
			answer["id_token"] = "jwt-" + strconv.Itoa(f.jwts)
			return build(req, jsonResp(200, answer)), nil
		}
		return ok("gigya_jwt")
	case "/api/auth/LoginJWT":
		r := jsonResp(200, fixture(f.tb, "login_jwt"))
		cookie := f.setCookie
		if cookie == "" {
			cookie = "cart=abc; Path=/"
		}
		r.headers = map[string]string{"Set-Cookie": cookie}
		return build(req, r), nil
	case "/api/shop/v6/Cart":
		return build(req, jsonResp(200, f.cart)), nil
	case "/1/indexes/*/queries":
		name := "algolia_products"
		if strings.Contains(f.searchParams(f.peekBody(req)).Get("filters"), "isInOffer") {
			name = "algolia_offers"
		}
		return build(req, jsonResp(200, obj{"results": []any{fixture(f.tb, name)}})), nil
	case "/api/shop/v6/ChangeLineCount":
		q := req.URL.Query()
		pid, count := q.Get("productId"), q.Get("count")
		if pid == "88672" {
			return ok("change_line_count_unavailable")
		}
		answer := fixture(f.tb, "change_line_count").(obj)
		applied, message := json.Number(count), ""
		if pid == "limited" {
			n, _ := strconv.Atoi(count)
			applied, message = json.Number(strconv.Itoa(min(n, 5))), "Max 5 pr. kunde"
		}
		answer["productId"], answer["productAmount"], answer["message"] = pid, applied, message
		return build(req, jsonResp(200, answer)), nil
	case "/api/shop/v3/AvailableDeliveryDates":
		return ok("available_delivery_dates")
	case "/api/shop/v7/OrderHistory":
		return ok("order_history")
	case "/api/shop/v7/OrderDetails":
		return ok("order_details_plain")
	}
	return build(req, emptyResp(404)), nil
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
	errReadError       = &netErr{text: "connection reset"}                       // httpx.ReadError
	errRemoteProtocol  = &netErr{text: "server closed"}                          // httpx.RemoteProtocolError
	errConnectTimeout  = &netErr{text: "connect timed out", timeout: true}       // httpx.ConnectTimeout
	errUnsupportedType = errors.New("unsupported protocol: not a network error") // httpx.UnsupportedProtocol
)
