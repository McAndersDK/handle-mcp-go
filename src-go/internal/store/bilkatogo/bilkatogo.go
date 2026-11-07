// Package bilkatogo is BilkaToGo: the private API bilkatogo.dk uses (docs/research.md).
//
// Login: Gigya accounts.login, then accounts.getJWT (24 h), then POST api.bilkatogo.dk/api/auth/LoginJWT.
// Search goes to the website's public Algolia index. Money comes in øre.
// Facts and fixtures from nickknissen/bilkatogo (MIT, Copyright (c) 2026 Nick Nissen).
// Never add order, payment or delivery-address calls here (docs/adr/0001).
//
// Go runs calls in parallel, where the Python adapter ran on one thread. Two locks keep it safe:
// lock is Python's asyncio.Lock (one login at a time, held across the network), and mu guards the
// fields below it for a moment only. Order: lock may be held while taking mu, never the other way
// round; mu is never held across a call; nothing takes lock twice.
package bilkatogo

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

const (
	name     = "bilkatogo"
	gigyaURL = "https://accounts.eu1.gigya.com"
	// Public keys from bilkatogo.dk's own JavaScript. Not secrets: every visitor's browser sends them.
	gigyaAPIKey    = "3_tA6BbV434FQqN73HnUG1KA3qFv8KiG4OqLu9eWPh7sKRqRizH5Vfv5Larmgrb4I2"
	gigyaBadLogin  = 403042
	gigyaRateLimit = 403048 // with 5xxxxx (Gigya's server errors): try later, the login is fine
	apiURL         = "https://api.bilkatogo.dk/api"
	algoliaURL     = "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/*/queries"
	algoliaIndex   = "prod_BILKATOGO_PRODUCTS"
	jwtSeconds     = 86400
	jwtMargin      = 300
	timeout        = 30 * time.Second
)

// The human picks home delivery or a pickup store on bilkatogo.dk; handle-mcp never does
// (docs/adr/0001, docs/adr/0006).
const (
	// notKnown: the cart's address does not say home or pickup (fail closed, docs/adr/0006).
	notKnown = "BilkaToGo viser ikke, om kurven er sat til hjemlevering eller afhentning. Vælg det på bilkatogo.dk."
	// noAddress: no store id at all, so there is no assortment to search.
	noAddress = "Ingen leveringsadresse valgt i BilkaToGo. Vælg hjemlevering eller afhentning på bilkatogo.dk."
)

// Public search-only key from the website's own JavaScript. Spelled as the website sends it.
var algoliaHeaders = [][2]string{
	{"x-algolia-application-id", "F9VBJLR1BK"},
	{"x-algolia-api-key", "1deaf41c87e729779f7695c00f190cc9"},
}

// gigyaRefused is a Gigya errorCode that is not "try later": a bad login or a dead login_token.
type gigyaRefused struct{ err error }

func (e *gigyaRefused) Error() string { return e.err.Error() }
func (e *gigyaRefused) Unwrap() error { return e.err }

// refused is Bilka's own error text in a 200 answer. SetQuantities reports it for that product
// and goes on; any other error stops the loop.
type refused struct{ err error }

func (e *refused) Error() string { return e.err.Error() }
func (e *refused) Unwrap() error { return e.err }

// Store is BilkaToGo. It implements store.Store.
type Store struct {
	client *http.Client
	sender *store.Sender
	jar    *cartJar
	now    func() time.Time
	site   siteState // the recipe pages (recipes.go): their own locks, never held with the two below

	lock sync.Mutex // see the package comment

	mu       sync.Mutex // guards everything below
	jwt      string
	token    string // the Gigya login_token: saved instead of the password
	jwtUntil time.Time
}

var _ store.Store = (*Store)(nil)

// Option changes a Store for tests.
type Option func(*Store)

// WithClock sets the clock the JWT lifetime and the delivery times read (default time.Now).
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithSleep sets how retries wait (default a real wait).
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(s *Store) { s.sender.Sleep = sleep }
}

// New makes the store. The client you pass is copied. It always gets its own cookie jar (the
// `cart` cookie must be forgettable), a 30 s timeout when it has none, and no redirects: Bilka's
// answers are never followed anywhere, as in the Python adapter.
func New(client *http.Client, opts ...Option) *Store {
	c := &http.Client{}
	if client != nil {
		cp := *client
		c = &cp
	}
	if c.Timeout == 0 {
		c.Timeout = timeout
	}
	inner := c.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	c.Transport = stripCart{inner}
	jar := newCartJar()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &Store{client: c, sender: &store.Sender{Client: c, Store: "BilkaToGo"}, jar: jar, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Name is "bilkatogo".
func (s *Store) Name() string { return name }

// Session is the Gigya login_token, saved instead of the password. nil before the first login.
func (s *Store) Session() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return nil
	}
	return map[string]string{"login_token": s.token}
}

// --- login --------------------------------------------------------------------

// gigya posts a form to Gigya. The HTTP status is not looked at: a bad login may come as 403 with
// JSON. The errorCode decides.
func (s *Store) gigya(ctx context.Context, method string, form pairs) (obj, error) {
	resp, err := s.send(ctx, call{method: "POST", url: gigyaURL + "/" + method, form: form})
	if err != nil {
		return nil, err
	}
	data, err := asDict(resp, method)
	if err != nil {
		return nil, err
	}
	if isNum(data["errorCode"], gigyaBadLogin) {
		return nil, &gigyaRefused{store.Errorf("BilkaToGo afviser login. Tjek email og kodeord.")}
	}
	code := withDefault(data, "errorCode", jsonZero)
	if f, _ := numVal(code); isNum(code, gigyaRateLimit) || (isPyInt(code) && f >= 500000) {
		return nil, store.Errorf("BilkaToGo-login svarer ikke lige nu. Prøv igen om lidt.")
	}
	if !isNum(code, 0) {
		msg := pyStr(withDefault(data, "errorMessage", data["errorCode"]))
		return nil, &gigyaRefused{store.Errorf("BilkaToGo login fejlede: %s", msg)}
	}
	return data, nil
}

func (s *Store) jwtFor(ctx context.Context, token string) (string, error) {
	form := pairs{{"apiKey", gigyaAPIKey}, {"login_token", token}, {"expiration", "86400"}}
	data, err := s.gigya(ctx, "accounts.getJWT", form)
	if err != nil {
		return "", err
	}
	jwt, _ := data["id_token"].(string)
	return jwt, nil
}

// login runs under lock.
func (s *Store) login(ctx context.Context) error {
	login, err := store.LoadLogin(ctx, name)
	if err != nil {
		return err
	}
	var token, jwt string
	if login.Session != nil { // saved by `handle-mcp login`: no password (docs/adr/0003)
		if token = login.Session["login_token"]; token == "" {
			return store.Expired(name)
		}
		var gr *gigyaRefused
		if jwt, err = s.jwtFor(ctx, token); errors.As(err, &gr) { // Gigya refuses the token: the session is over
			return store.Expired(name)
		} else if err != nil {
			return err
		}
	} else {
		form := pairs{{"loginID", login.Email}, {"password", login.Password}, {"apiKey", gigyaAPIKey}, {"httpStatusCodes", "true"}}
		data, err := s.gigya(ctx, "accounts.login", form)
		if err != nil {
			return err
		}
		token, _ = objOrEmpty(data["sessionInfo"])["cookieValue"].(string)
		if token != "" {
			if jwt, err = s.jwtFor(ctx, token); err != nil {
				return err
			}
		}
	}
	if token == "" || jwt == "" {
		return store.Errorf("BilkaToGo login svarede uden token. API'et kan have ændret sig.")
	}
	// Never send the `cart` cookie: LoginJWT would REPLACE the account's cart with it.
	s.jar.forgetCart()
	resp, err := s.send(withoutCart(ctx), call{method: "POST", url: apiURL + "/auth/LoginJWT",
		query: pairs{{"u", "w"}}, header: jwtHeader(jwt)})
	if err != nil {
		return err
	}
	if resp.Status != http.StatusOK {
		return store.Errorf("BilkaToGo login fejlede (%d).", resp.Status)
	}
	s.mu.Lock()
	s.jwt, s.token = jwt, token
	s.jwtUntil = s.now().Add(time.Duration(jwtSeconds-jwtMargin) * time.Second)
	s.mu.Unlock()
	slog.Info("logged in to BilkaToGo")
	if login.Password != "" { // an old saved password is replaced by the session
		store.SaveSession(ctx, name, login.Email, map[string]string{"login_token": token})
	}
	return nil
}

func jwtHeader(jwt string) http.Header { return http.Header{"jwt_token": {jwt}} } // spelled as Bilka expects

// ensureJWT logs in when there is no JWT, it is about to end, or `fresh` asks for a new one. It
// skips that login when another call has already logged in since the attempt that failed: `used` is
// the JWT that attempt carried, and it is no longer the current one. (Python took the current JWT
// before it waited for lock, which could repeat a login another call had just made; one thread
// rarely met that, parallel calls would.) It returns the JWT to send.
func (s *Store) ensureJWT(ctx context.Context, fresh bool, used string) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.mu.Lock()
	jwt, until := s.jwt, s.jwtUntil
	s.mu.Unlock()
	stale := fresh && jwt == used
	if stale || jwt == "" || !s.now().Before(until) {
		if err := s.login(ctx); err != nil {
			return "", err
		}
		s.mu.Lock()
		jwt = s.jwt
		s.mu.Unlock()
	}
	return jwt, nil
}

// shop is a logged-in call to api.bilkatogo.dk. Bilka answers HTTP 200 on errors: {"code": 1} is
// "not logged in" (log in again, once), any other msg with a code is a refusal.
func (s *Store) shop(ctx context.Context, method, path string, params pairs) (obj, error) {
	sent := "" // the JWT the last attempt carried
	for _, fresh := range []bool{false, true} {
		jwt, err := s.ensureJWT(ctx, fresh, sent)
		if err != nil {
			return nil, err
		}
		sent = jwt
		resp, err := s.send(ctx, call{method: method, url: apiURL + "/" + path,
			query: append(pairs{{"u", "w"}}, params...), header: jwtHeader(jwt)})
		if err != nil {
			return nil, err
		}
		if resp.Status == http.StatusUnauthorized {
			continue
		}
		if resp.Status >= 400 {
			return nil, store.Errorf("BilkaToGo afviste %s (%d).", path, resp.Status)
		}
		data, err := asDict(resp, path)
		if err != nil {
			return nil, err
		}
		if isNum(data["code"], 1) { // {"code": 1, "msg": "Du er ikke logget ind"}
			continue
		}
		if has(data, "msg") && has(data, "code") {
			return nil, &refused{store.Errorf("%s", pyStr(data["msg"]))}
		}
		return data, nil
	}
	return nil, store.Errorf("BilkaToGo afviser en frisk session. Tjek login.")
}

// cartJSON is the cart with its delivery address and chosen delivery time.
func (s *Store) cartJSON(ctx context.Context) (obj, error) {
	return s.shop(ctx, "GET", "shop/v6/Cart", pairs{{"extra", "deliveryAddress,deliveryDate"}})
}
