package rema1000

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func passwordCtx() context.Context {
	return store.WithLogin(context.Background(), name, store.Login{Email: "kunde@example.com", Password: "hemmeligt"})
}

func TestPasswordLoginFollowsTheOAuthHopsAndKeepsTwoCookies(t *testing.T) {
	isolate(t)
	f := newFake(t)
	s := newStore(f)
	if _, _, err := s.Choices(passwordCtx()); err != nil {
		t.Fatal(err)
	}
	session := s.Session()
	if session[accessCookie] != f.access || session[refreshCookie] != "refresh-login" || len(session) != 2 {
		t.Errorf("session: %v", session)
	}
	var hosts []string
	for _, r := range f.requests() {
		hosts = append(hosts, r.Method+" "+r.Host+r.Path)
		if strings.Contains(r.Body, "hemmeligt") && !strings.HasSuffix(r.Path, "/oauth2/login") {
			t.Errorf("the password went to %s", r.Path)
		}
	}
	want := []string{"GET shop.rema1000.dk/konto/auth", "GET login.rema1000.dk/login", "POST cphapp.rema1000.dk/api/oauth2/login",
		"GET api.digital.rema1000.dk/oauth/authorize", "GET shop.rema1000.dk" + callbackPath}
	if strings.Join(hosts[:5], "|") != strings.Join(want, "|") {
		t.Errorf("hops: %v", hosts)
	}
	for _, r := range f.requests() {
		if r.Path == "/konto" {
			t.Error("the login followed the callback's own redirect")
		}
	}
	if f.requests()[2].Body != `{"email":"kunde@example.com","password":"hemmeligt"}` {
		t.Errorf("login body: %s", f.requests()[2].Body)
	}
}

func TestBadPasswordIsClear(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.loginOK = false
	_, _, err := newStore(f).Choices(passwordCtx())
	wantStoreError(t, err, "afviser login")
}

func TestQueueIsReportedNotWorkedAround(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.queueOnLogin = true
	_, _, err := newStore(f).Choices(passwordCtx())
	wantStoreError(t, err, "kø eller et bot-tjek")
	for _, r := range f.requests() {
		if strings.Contains(r.Host, "queue-it") {
			t.Error("a request went to the queue")
		}
	}
}

func TestSavedSessionIsUsedWithoutALogin(t *testing.T) {
	f, s := harness(t)
	if _, err := s.Search(context.Background(), "mælk", 2); err != nil {
		t.Fatal(err)
	}
	if f.count("POST", "") != 0 || f.count("GET", "/konto/auth") != 0 {
		t.Errorf("unexpected login or refresh: %v", f.requests())
	}
	for _, r := range f.requests() {
		if r.Path == "/api/v3/stores" && r.Auth != "Bearer "+f.access {
			t.Errorf("auth header: %q", r.Auth)
		}
		if strings.HasPrefix(r.Path, "/api/search/") && r.Auth != "" {
			t.Errorf("a public call carried the token: %q", r.Auth)
		}
	}
}

func TestEndedAccessTokenIsRenewedAndTheRotatedCookieSaved(t *testing.T) {
	isolate(t)
	f := newFake(t)
	old := makeJWT(testNow.Add(-time.Minute), userID) // ended
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", "store_id": "9100"})
	f.rotate = "refresh-2"
	s := newStore(f)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.count("POST", "/api/auth/refresh") != 1 {
		t.Errorf("refreshes: %d", f.count("POST", "/api/auth/refresh"))
	}
	saved := savedSession(t)
	if saved[accessCookie] != f.nextAccess || saved[refreshCookie] != "refresh-2" || saved["store_id"] != "9100" {
		t.Errorf("saved session: %v", saved)
	}
	if f.count("POST", "/oauth2/login") != 0 {
		t.Error("a password login was made")
	}
}

func TestA401RenewsOnceAndRetries(t *testing.T) {
	f, s := harness(t)
	if _, err := s.GetCart(context.Background()); err != nil { // logged in, gate checked
		t.Fatal(err)
	}
	f.access = makeJWT(testNow.Add(3*time.Hour), userID) // the server forgot our token
	f.nextAccess = f.access
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("%d refreshes", n)
	}
}

func TestDeadSessionSaysLogInAgain(t *testing.T) {
	f, s := harness(t)
	f.refreshCookie = "" // the refresh cookie is no longer accepted
	f.access = makeJWT(testNow.Add(time.Hour), userID+1)
	_, err := s.GetCart(context.Background())
	wantStoreError(t, err, "Login til rema1000 er udløbet")
	wantStoreError(t, err, "handle-mcp login rema1000")
}

func TestJWTClaims(t *testing.T) {
	exp, sub := claims(makeJWT(testNow, 42))
	if !exp.Equal(testNow.Truncate(time.Second)) || sub != 42 {
		t.Errorf("exp %v sub %d", exp, sub)
	}
	if exp, sub := claims("not-a-jwt"); !exp.IsZero() || sub != 0 {
		t.Error("a non-JWT must give zero values")
	}
}
