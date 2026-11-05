// Package nemlig is nemlig.com: the private web API the website uses (docs/research.md).
//
// Login: XSRF token, then a short Bearer token (5 min), then POST /webapi/login, which sets the
// .ASPXAUTH cookie (1 year). The Bearer token is renewed on 401.
// Never add order, payment or delivery-time endpoints here (docs/adr/0001).
//
// Go runs calls in parallel, where the Python adapter ran on one thread. Two locks keep it safe:
// lock is Python's asyncio.Lock (one login or token renewal at a time, held across the network),
// and mu guards the fields below it for a moment only. Order: lock may be held while taking mu,
// never the other way round; mu is never held across a call; nothing takes lock twice.
package nemlig

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // Danish time must work without a zoneinfo folder on the machine

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

const (
	name           = "nemlig"
	base           = "https://www.nemlig.com"
	searchURL      = "https://webapi.prod.knl.nemlig.it/searchgateway/api/search"
	bff            = "https://webapi.prod.knl.nemlig.it/productbff/api/web"
	defaultVersion = "11.201.0"
	settingsTTL    = 600 * time.Second // reuse timeslot and zone for search
	authCookie     = ".ASPXAUTH"       // set by /webapi/login, lives 1 year
	freeCampaign   = "ProductCampaignFreeProduct"
)

var baseURL = &url.URL{Scheme: "https", Host: "www.nemlig.com", Path: "/"}

// errUnauthorized is HTTP 401: the Bearer token or the login cookie expired. It never leaves
// this package.
var errUnauthorized = errors.New("nemlig.com 401")

// refusedLogin is a login nemlig refused: a wrong email or password, or a dead saved session. It
// wraps the StoreError the human reads, so errors.As to *store.StoreError still works. The
// settings step does not swallow it, because Search would then log in a second time, and
// repeated attempts can lock the account (docs/adr/0005). A network failure or a 5xx is not this.
type refusedLogin struct{ err error }

func (e *refusedLogin) Error() string { return e.err.Error() }
func (e *refusedLogin) Unwrap() error { return e.err }

func refused(err error) error { return &refusedLogin{err} }

// Store is nemlig.com. It implements store.Store.
type Store struct {
	jar     http.CookieJar
	sender  *store.Sender
	version string // the Version header: HANDLE_NEMLIG_APP_VERSION, read once
	now     func() time.Time

	lock sync.Mutex // see the package comment

	mu       sync.Mutex // guards everything below
	bearer   string
	xsrf     string
	gen      int // changes whenever bearer or xsrf does: a 401 for an old one is already handled
	loggedIn bool
	resumed  bool // logged in with a saved session, not a password
	settings *settingsCache
	slotID   string // the basket's delivery time, for productbff calls
	tags     map[string]string
	tagNames []string
}

var _ store.Store = (*Store)(nil)

// Option changes a Store for tests.
type Option func(*Store)

// WithClock sets the clock the settings cache reads (default time.Now).
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithSleep sets how retries wait (default a real wait).
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(s *Store) { s.sender.Sleep = sleep }
}

// New makes the store. A nil client gets a cookie jar, a 20 s timeout and up to 20 redirects,
// as the Python adapter's client had. A client you pass is copied, and given those three things
// where it has none: the login cookie lives in the jar.
func New(client *http.Client, opts ...Option) *Store {
	c := &http.Client{Timeout: 20 * time.Second}
	if client != nil {
		cp := *client
		c = &cp
	}
	if c.Timeout == 0 {
		c.Timeout = 20 * time.Second
	}
	if c.Jar == nil {
		jar, _ := cookiejar.New(nil) // New(nil) never fails
		c.Jar = jar
	}
	if c.CheckRedirect == nil {
		c.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 20 {
				return errors.New("too many redirects")
			}
			return nil
		}
	}
	version, ok := os.LookupEnv("HANDLE_NEMLIG_APP_VERSION")
	if !ok {
		version = defaultVersion
	}
	s := &Store{
		jar:     c.Jar,
		sender:  &store.Sender{Client: c, Store: "nemlig.com"},
		version: version,
		now:     time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Name is "nemlig".
func (s *Store) Name() string { return name }

// --- HTTP -------------------------------------------------------------------

// call is one request. Params keep their order.
type call struct {
	method string
	url    string
	params params
	body   any // sent as JSON
}

type params [][2]string

func (p params) add(key string, value any) params { return append(p, [2]string{key, paramStr(value)}) }

// paramStr is how a query value is written: as httpx wrote it.
func paramStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return string(x)
	}
	return pyStr(v)
}

func (c call) fullURL() string {
	if len(c.params) == 0 {
		return c.url
	}
	parts := make([]string, len(c.params))
	for i, kv := range c.params {
		parts[i] = url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1])
	}
	return c.url + "?" + strings.Join(parts, "&")
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { // never fails on a working system
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// headers are the website's own, and which token generation they carry.
func (s *Store) headers() (http.Header, int) {
	h := http.Header{}
	set := func(k, v string) { h[k] = []string{v} } // exact spelling, not canonicalised
	set("Accept", "application/json, text/plain, */*")
	set("Device-Size", "desktop")
	set("Platform", "web")
	set("Version", s.version)
	set("X-Correlation-Id", newUUID())
	set("Referer", base+"/")
	set("User-Agent", "Mozilla/5.0 (handle-mcp)")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bearer != "" {
		set("Authorization", "Bearer "+s.bearer)
	}
	if s.xsrf != "" {
		set("X-XSRF-TOKEN", s.xsrf)
	}
	return h, s.gen
}

func isQueue(finalURL string) bool {
	u, err := url.Parse(finalURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "queue-it.net" || strings.HasSuffix(host, ".queue-it.net")
}

// send is one request with retries on timeout, 429 and 5xx. It returns the parsed JSON (nil for
// an empty body) and the token generation the request carried. A queue-it host in the final URL,
// after redirects, is a clear error: we never work around a queue.
func (s *Store) send(ctx context.Context, c call) (any, int, error) {
	h, gen := s.headers()
	req := store.Request{Method: c.method, URL: c.fullURL(), Header: h}
	if c.body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(c.body); err != nil {
			return nil, gen, store.Errorf("nemlig.com: kunne ikke bygge kaldet.")
		}
		req.Body = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		h["Content-Type"] = []string{"application/json"}
	}
	resp, err := s.sender.Send(ctx, req)
	if err != nil {
		return nil, gen, err
	}
	if isQueue(resp.URL) {
		return nil, gen, store.Errorf("nemlig.com har kø lige nu (queue-it). Prøv igen om lidt.")
	}
	if resp.Status == http.StatusUnauthorized {
		return nil, gen, errUnauthorized
	}
	if resp.Status >= 400 { // the URL may hold ids: stderr only, path without query
		slog.Warn("nemlig.com request refused", "method", c.method, "path", pathOf(resp.URL), "status", resp.Status)
		return nil, gen, store.Errorf("nemlig.com afviste kaldet (%d).", resp.Status)
	}
	if len(resp.Body) == 0 {
		return nil, gen, nil
	}
	v, err := decodeJSON(resp.Body)
	if err != nil {
		return nil, gen, store.Errorf("nemlig.com svarede ikke med JSON. API'et kan have ændret sig.")
	}
	return v, gen, nil
}

func pathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// api is a logged-in call. On 401: renew the Bearer token, then log in again.
func (s *Store) api(ctx context.Context, c call) (any, error) {
	if err := s.ensureLogin(ctx); err != nil {
		return nil, err
	}
	for _, renew := range []func(context.Context) error{s.renewToken, s.login, nil} {
		data, gen, err := s.send(ctx, c)
		if !errors.Is(err, errUnauthorized) {
			return data, err
		}
		if renew == nil {
			if s.isResumed() {
				return nil, refused(store.Expired(name))
			}
			return nil, refused(store.Errorf("nemlig.com afviser login. Tjek email og kodeord."))
		}
		if err := s.renewAfter(ctx, gen, renew); err != nil && !errors.Is(err, errUnauthorized) {
			return nil, err
		}
	}
	panic("unreachable")
}

func (s *Store) ensureLogin(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.isLoggedIn() {
		return nil
	}
	if err := s.login(ctx); errors.Is(err, errUnauthorized) {
		return refused(store.Errorf("nemlig.com afviser login. Tjek email og kodeord."))
	} else if err != nil {
		return err
	}
	return nil
}

// renewAfter runs a renewal unless another call already renewed since our request went out.
func (s *Store) renewAfter(ctx context.Context, gen int, renew func(context.Context) error) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.generation() != gen {
		return nil
	}
	return renew(ctx)
}

// hasLogin is true when logged in, or a login is saved. Once logged in the keychain is not read.
func (s *Store) hasLogin(ctx context.Context) bool {
	return s.isLoggedIn() || store.HasLogin(ctx, name)
}

// public is a call that needs no login (offers, settings): logged in when a login is saved, so
// prices and delivery time fit the household; else with an anonymous token, renewed once when
// nemlig says it has expired. Search is not such a call: with an anonymous token the search
// gateway answers 500 (live 2026-11-04).
func (s *Store) public(ctx context.Context, c call) (any, error) {
	if s.hasLogin(ctx) {
		return s.api(ctx, c)
	}
	for last := false; ; last = true {
		data, gen, err := s.publicOnce(ctx, c)
		if !errors.Is(err, errUnauthorized) {
			return data, err
		}
		if last {
			return nil, store.Errorf("nemlig.com afviser kaldet uden login. Gem et login: handle-mcp login nemlig")
		}
		s.dropBearer(gen) // expired: the next round fetches a fresh one
	}
}

func (s *Store) publicOnce(ctx context.Context, c call) (any, int, error) {
	s.lock.Lock()
	var err error
	if !s.hasBearer() {
		err = s.renewToken(ctx)
	}
	s.lock.Unlock()
	if err != nil {
		return nil, -1, err
	}
	return s.send(ctx, c)
}

// tokenField reads a required text from the token calls.
func tokenField(v any, key string) (string, error) {
	if m, ok := v.(obj); ok {
		if s, ok := m[key].(string); ok && s != "" {
			return s, nil
		}
	}
	return "", store.Errorf("nemlig.com svarede ikke som forventet ved login. API'et kan have ændret sig.")
}

func (s *Store) renewToken(ctx context.Context) error {
	s.setAuth(func() { s.bearer = "" })
	tok, _, err := s.send(ctx, call{method: "GET", url: base + "/webapi/Token"})
	if err != nil {
		return err
	}
	bearer, err := tokenField(tok, "access_token")
	if err != nil {
		return err
	}
	s.setAuth(func() { s.bearer = bearer })
	af, _, err := s.send(ctx, call{method: "GET", url: base + "/webapi/AntiForgery"})
	if err != nil {
		return err
	}
	xsrf, err := tokenField(af, "Value")
	if err != nil {
		return err
	}
	s.setAuth(func() { s.xsrf = xsrf })
	return nil
}

var loginFlags = obj{"CheckForExistingProducts": true, "DoMerge": true, "AppInstalled": false, "SaveExistingBasket": false}

// login runs under lock. It may return errUnauthorized (a token call refused after the login).
func (s *Store) login(ctx context.Context) error {
	login, err := store.LoadLogin(ctx, name)
	if err != nil {
		return err
	}
	s.setAuth(func() { s.bearer, s.xsrf = "", "" })
	if login.Session != nil { // saved by `handle-mcp login`: no password (docs/adr/0003)
		return s.resume(ctx, login.Session)
	}
	body := obj{"Username": login.Email, "Password": login.Password}
	for k, v := range loginFlags {
		body[k] = v
	}
	err = s.renewToken(ctx)
	var result any
	if err == nil {
		result, _, err = s.send(ctx, call{method: "POST", url: base + "/webapi/login", body: body})
	}
	if errors.Is(err, errUnauthorized) {
		return refused(store.Errorf("nemlig.com afviser login. Tjek email og kodeord."))
	} else if err != nil {
		return err
	}
	if m, ok := result.(obj); !ok || !has(m, "RedirectUrl") {
		return refused(store.Errorf("nemlig.com login fejlede. Tjek email og kodeord."))
	}
	if err := s.renewToken(ctx); err != nil { // tokens change after login
		return err
	}
	s.mu.Lock()
	s.loggedIn = true
	s.mu.Unlock()
	slog.Info("logged in to nemlig.com")
	if session := s.Session(); session != nil { // an old saved password is replaced by the session
		store.SaveSession(ctx, name, login.Email, session)
	}
	return nil
}

func has(m obj, key string) bool { _, ok := m[key]; return ok }

// resume logs in with the saved login cookie. A dead cookie gives an anonymous basket (no email),
// not a 401: check it, never shop anonymously by mistake.
func (s *Store) resume(ctx context.Context, session map[string]string) error {
	value := session[authCookie]
	if value == "" {
		return refused(store.Expired(name))
	}
	s.jar.SetCookies(baseURL, []*http.Cookie{{Name: authCookie, Value: value, Domain: ".nemlig.com", Path: "/", Secure: true, HttpOnly: true}})
	if !s.cookieReadable(value) {
		slog.Warn("nemlig: the cookie jar did not keep the login cookie")
		return refused(store.Expired(name))
	}
	err := s.renewToken(ctx)
	var basket any
	if err == nil {
		basket, _, err = s.send(ctx, call{method: "GET", url: base + "/webapi/basket/GetBasket"})
	}
	if errors.Is(err, errUnauthorized) {
		return refused(store.Expired(name))
	} else if err != nil {
		return err
	}
	if m, ok := basket.(obj); !ok || !truthy(m["Email"]) {
		return refused(store.Expired(name))
	}
	s.mu.Lock()
	s.loggedIn, s.resumed = true, true
	s.mu.Unlock()
	slog.Info("resumed the nemlig.com session")
	return nil
}

func (s *Store) cookieReadable(value string) bool {
	for _, c := range s.jar.Cookies(baseURL) {
		if c.Name == authCookie && c.Value == value {
			return true
		}
	}
	return false
}

// Session is the login cookie (1 year), saved instead of the password. nil when there is none.
func (s *Store) Session() map[string]string {
	for _, c := range s.jar.Cookies(baseURL) {
		if c.Name == authCookie {
			if c.Value == "" {
				return nil
			}
			return map[string]string{authCookie: c.Value}
		}
	}
	return nil
}

// --- state, always through mu ------------------------------------------------

func (s *Store) setAuth(change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
	s.gen++
}

func (s *Store) dropBearer(gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen == -1 || gen == s.gen {
		s.bearer = ""
		s.gen++
	}
}

func (s *Store) generation() int { s.mu.Lock(); defer s.mu.Unlock(); return s.gen }
func (s *Store) hasBearer() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.bearer != "" }
func (s *Store) isLoggedIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loggedIn
}
func (s *Store) isResumed() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.resumed }
