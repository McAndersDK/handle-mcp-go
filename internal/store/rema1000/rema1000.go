// Package rema1000 is REMA 1000: the private API the webshop uses (docs/research.md). The webshop
// is pickup only (Køb & Hent), and the basket is a shopping list (docs/adr/0008).
//
// Login: the OAuth login page, one POST with email and password, and the site callback, which
// sets the cookies access_token (a JWT, about an hour) and refresh_token. The session saved
// after a login is those two cookies and the pickup store the human chose; the password is
// forgotten. The access token is renewed with the refresh cookie (auth.go).
//
// The adapter works only with a pickup store that has Køb & Hent, chosen on this machine with
// `handle-mcp login rema1000` (gate.go). Nothing on REMA's side is ever set. The only lists it
// writes to is today's "handle-mcp <date>" (cart.go). Never add order, payment, cart, store, time
// or coupon calls here (docs/adr/0001, docs/adr/0008).
//
// Locks, as in the other stores: lock is one login or renewal at a time, held across the
// network; mu guards the fields below it for a moment only; gateLock is held across the one
// store check; writeLock is held by one SetQuantities from finding today's list to the read-back.
// Order: writeLock or gateLock (never both), then lock, then mu. Never the other way round.
package rema1000

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
	_ "time/tzdata" // Danish time must work without a zoneinfo folder on the machine

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

const (
	name        = "rema1000"
	display     = "REMA 1000"
	apiBase     = "https://api.digital.rema1000.dk/api"
	shopHost    = "shop.rema1000.dk"
	loginHost   = "login.rema1000.dk"
	appHost     = "cphapp.rema1000.dk"
	apiHost     = "api.digital.rema1000.dk"
	shopBase    = "https://" + shopHost
	siteRecipes = shopBase + "/opskrifter/"
	timeout     = 30 * time.Second
	tokenMargin = 2 * time.Minute // renew this long before the access token ends
	// advertisedLabel is REMA's own label for the products in the weekly leaflet (Avisvare).
	advertisedLabel = "9001"
	// storeEnv overrides the pickup store saved by `handle-mcp login rema1000`.
	storeEnv = "HANDLE_REMA1000_STORE_ID"
)

var (
	dk       = mustLocation("Europe/Copenhagen")
	shopURL  = &url.URL{Scheme: "https", Host: shopHost, Path: "/"}
	errQueue = store.Errorf("REMA 1000 har kø eller et bot-tjek lige nu. Prøv igen senere.")
	errBot   = store.Errorf("REMA 1000 afviste forespørgslen (403): kø eller bot-tjek. Prøv igen senere.")
	// errRefused is a login or renewal REMA refused: a wrong password or a dead session. It is
	// not a network failure or a 5xx, which are StoreErrors on their own.
	errRefused = errors.New("rema1000 refused")
)

func mustLocation(n string) *time.Location {
	loc, err := time.LoadLocation(n)
	if err != nil {
		panic(err) // the tzdata import makes this impossible
	}
	return loc
}

// Store is REMA 1000. It implements store.Store and store.Chooser.
type Store struct {
	sender *store.Sender
	jar    http.CookieJar
	now    func() time.Time

	writeLock sync.Mutex // one SetQuantities at a time, from finding today's list to the read-back
	gateLock  sync.Mutex // see the package comment
	lock      sync.Mutex

	mu         sync.Mutex // guards everything below
	token      string     // the access token as a Bearer value
	tokenExp   time.Time
	userID     int64
	cookies    map[string]string // access_token and refresh_token, raw, as saved in the session
	savedStore string            // the pickup store id in the saved session
	gate       *pickupStore      // set once the store has been checked
}

var (
	_ store.Store   = (*Store)(nil)
	_ store.Chooser = (*Store)(nil)
)

// Option changes a Store for tests.
type Option func(*Store)

// WithClock sets the clock the token lifetime and the dates read (default time.Now).
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithSleep sets how retries wait (default a real wait).
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(s *Store) { s.sender.Sleep = sleep }
}

// New makes the store. The client you pass is copied. It always gets its own cookie jar (the
// login lives in it), a 30 s timeout when it has none, and no redirects: the login follows its
// own hops one at a time, and an API answer that redirects is a queue or a bot check.
func New(client *http.Client, opts ...Option) *Store {
	c := &http.Client{}
	if client != nil {
		cp := *client
		c = &cp
	}
	if c.Timeout == 0 {
		c.Timeout = timeout
	}
	jar, _ := cookiejar.New(nil) // New(nil) never fails
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &Store{sender: &store.Sender{Client: c, Store: display}, jar: jar, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Name is "rema1000".
func (s *Store) Name() string { return name }

// Session is what to save after a login: the two cookies and the chosen pickup store. Never the
// password. nil before the first login.
func (s *Store) Session() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cookies) == 0 {
		return nil
	}
	out := make(map[string]string, 3)
	for k, v := range s.cookies {
		out[k] = v
	}
	if s.savedStore != "" {
		out["store_id"] = s.savedStore
	}
	return out
}

// today is the Copenhagen calendar day, as 2026-11-07.
func (s *Store) today() string { return s.now().In(dk).Format("2006-01-02") }
