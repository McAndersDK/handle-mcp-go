package rema1000

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"
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
	if _, _, err := s.Choices(passwordCtx(), ""); err != nil {
		t.Fatal(err)
	}
	session := s.Session()
	if session[accessCookie] != f.access || session[refreshCookie] != "refresh-login" || session[apiCookie] != "api-login" ||
		session[awsCookie] != "lb-login" || session[awsCorsCookie] != "lbc-login" || session[expKey] == "" || len(session) != 6 {
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
	_, _, err := newStore(f).Choices(passwordCtx(), "")
	wantStoreError(t, err, "afviser login")
}

func TestQueueIsReportedNotWorkedAround(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.queueOnLogin = true
	_, _, err := newStore(f).Choices(passwordCtx(), "")
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
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue, "store_id": "9100"})
	f.rotate = "refresh-2"
	s := newStore(f)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.count("POST", "/api/auth/refresh") != 1 {
		t.Errorf("refreshes: %d", f.count("POST", "/api/auth/refresh"))
	}
	saved := savedSession(t)
	if saved[accessCookie] != f.nextAccess || saved[refreshCookie] != "refresh-2" || saved[savedKey] != "9100" {
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

func TestRenewalSendsAllThreeCookiesAndSavesTheRotatedOnes(t *testing.T) {
	isolate(t)
	f := newFake(t)
	old := makeJWT(testNow.Add(-time.Minute), userID) // ended
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue, "store_id": "9100"})
	f.rotate = "refresh-2"
	f.rotateAPI = "api-2"
	if _, err := newStore(f).GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := ""
	for _, r := range f.requests() {
		if r.Path == "/api/auth/refresh" {
			sent = r.Cookie
		}
	}
	for _, want := range []string{accessCookie + "=" + old, refreshCookie + "=refresh-1", apiCookie + "=" + apiValue, awsCookie + "=" + lbValue} {
		if !strings.Contains(sent, want) {
			t.Errorf("the refresh call sent %q, without %q", sent, want)
		}
	}
	saved := savedSession(t)
	if saved[accessCookie] != f.nextAccess || saved[refreshCookie] != "refresh-2" || saved[apiCookie] != "api-2" {
		t.Errorf("saved session: %v", saved)
	}
}

// An old session without api_session: the renewal is tried once, REMA refuses it (500), and the
// human is told to log in again. The call is not retried.
func TestOldSessionWithoutApiSessionSaysLogInAgain(t *testing.T) {
	isolate(t)
	f := newFake(t)
	old := makeJWT(testNow.Add(-time.Minute), userID) // ended
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", "store_id": "9100"})
	_, err := newStore(f).GetCart(context.Background())
	wantStoreError(t, err, "Login til rema1000 er udløbet")
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("refreshes: %d, want one", n)
	}
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

// clockStore is a store on the fake with a clock the test moves. It has no saved session.
func clockStore(t *testing.T, f *fake) (*Store, *clock) {
	t.Helper()
	c := &clock{now: testNow}
	s := New(&http.Client{Transport: f},
		WithClock(func() time.Time { return c.now }),
		WithSleep(func(context.Context, time.Duration) error { return nil }))
	return s, c
}

// calls makes n Choices calls, each of which needs a valid token.
func calls(t *testing.T, s *Store, ctx context.Context, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, _, err := s.Choices(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
}

// The key regression. A fresh login gives an access token whose exp is a float, which the old
// reader could not parse, so every call renewed the session. The login and ten calls make no
// renewal; a call once the margin is reached makes exactly one.
func TestFreshLoginMakesNoRenewalUntilTheMargin(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.access = jwtWith(obj{"exp": float64(testNow.Add(time.Hour).Unix()) + 0.5, "sub": userID})
	f.refreshCookie = "refresh-login" // the refresh cookie the callback sets
	s, c := clockStore(t, f)
	ctx := passwordCtx()
	calls(t, s, ctx, 11) // the login, then ten calls
	if got, want := s.Session()[expKey], strconv.FormatInt(testNow.Add(time.Hour).Unix(), 10); got != want {
		t.Errorf("access_exp %q, want the token's exp %q", got, want) // read from exp, not a fallback
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 0 {
		t.Fatalf("%d renewals after a fresh login, want 0", n)
	}
	c.advance(time.Hour - tokenMargin + time.Second) // within the margin of the end
	calls(t, s, ctx, 10)
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("%d renewals once the margin was reached, want 1", n)
	}
	if n := f.count("POST", "/oauth2/login"); n != 1 {
		t.Errorf("%d password logins, want 1", n)
	}
}

// The token has no exp: the access cookie's Max-Age gives the end, and a renewal gives a new one.
func TestNoExpUsesTheCookieMaxAge(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.access = jwtWith(obj{"sub": userID})
	f.nextAccess = jwtWith(obj{"sub": userID})
	f.accessMaxAge = 600
	f.refreshCookie = "refresh-login"
	s, c := clockStore(t, f)
	ctx := passwordCtx()
	calls(t, s, ctx, 11)
	if n := f.count("POST", "/api/auth/refresh"); n != 0 {
		t.Fatalf("%d renewals within the Max-Age, want 0", n)
	}
	c.advance(10*time.Minute - tokenMargin + time.Second)
	calls(t, s, ctx, 10)
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("%d renewals at the margin, want 1", n)
	}
}

// An old session has no access_exp, and its token has no exp: the end comes from the time the
// session was loaded, and it is saved from then on.
func TestOldSessionWithoutAccessExpGetsAnEnd(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.access = jwtWith(obj{"sub": userID})
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue, "store_id": strconv.Itoa(storeID)})
	s, _ := clockStore(t, f)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 0 {
		t.Errorf("%d renewals, want 0", n)
	}
	if got, want := s.Session()[expKey], strconv.FormatInt(testNow.Add(fallbackLife).Unix(), 10); got != want {
		t.Errorf("access_exp %q, want %q", got, want)
	}
}

// A saved access_exp is used as it is: a session that ends within the margin is renewed at once.
func TestSavedAccessExpIsUsed(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.access = jwtWith(obj{"sub": userID}) // no exp of its own
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue,
		expKey: strconv.FormatInt(testNow.Add(time.Minute).Unix(), 10), "store_id": strconv.Itoa(storeID)})
	s, _ := clockStore(t, f)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("%d renewals, want 1", n)
	}
}

// A restored session renews, sends the saved load balancer cookie, and saves the rotated ones.
func TestRestoredSessionRenewsAndSavesTheRotatedAWSALB(t *testing.T) {
	isolate(t)
	f := newFake(t)
	old := makeJWT(testNow.Add(-time.Minute), userID) // ended
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", apiCookie: apiValue, awsCookie: "lb-old", awsCorsCookie: "lbc-old", "store_id": strconv.Itoa(storeID)})
	if _, err := newStore(f).GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Fatalf("%d renewals, want 1", n)
	}
	for _, r := range f.requests() {
		if r.Path == "/api/auth/refresh" && !strings.Contains(r.Cookie, awsCookie+"=lb-old") {
			t.Errorf("the refresh sent %q, without the saved AWSALB", r.Cookie)
		}
	}
	saved := savedSession(t)
	if saved[awsCookie] != "lb-1" || saved[awsCorsCookie] != "lbc-1" || saved[accessCookie] != f.nextAccess || saved[expKey] == "" {
		t.Errorf("saved session: %v", saved)
	}
}

// An old session saved before the load balancer cookies were kept: the refresh is tried once, REMA
// refuses it, and the human is told to log in again. It is not reported as a 500 from REMA.
func TestOldSessionWithoutAWSALBSaysLogInAgain(t *testing.T) {
	isolate(t)
	f := newFake(t)
	old := makeJWT(testNow.Add(-time.Minute), userID) // ended
	saveSession(t, map[string]string{accessCookie: old, refreshCookie: "refresh-1", apiCookie: apiValue, "store_id": strconv.Itoa(storeID)})
	_, err := newStore(f).GetCart(context.Background())
	wantStoreError(t, err, "Login til rema1000 er udløbet")
	if strings.Contains(err.Error(), "svarer") {
		t.Errorf("an old session got a store error: %v", err)
	}
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("refreshes: %d, want one", n)
	}
}

// The load balancer cookies go to the shop only: no call to the API carries them.
func TestLoadBalancerCookiesNeverReachTheAPI(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.refreshCookie = "refresh-login"
	s, _ := clockStore(t, f)
	calls(t, s, passwordCtx(), 3)
	api := 0
	for _, r := range f.requests() {
		if r.Host != apiHost {
			continue
		}
		api++
		if strings.Contains(r.Cookie, awsCookie+"=") || strings.Contains(r.Cookie, awsCorsCookie+"=") {
			t.Errorf("an API call carried the load balancer cookies: %q", r.Cookie)
		}
	}
	if api == 0 {
		t.Fatal("no API call was made")
	}
}

// The logs name the source of the end and the claims and cookies, never a value.
func TestLogsNameTheSourceAndNeverAValue(t *testing.T) {
	isolate(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	f := newFake(t)
	f.access = jwtWith(obj{"sub": userID, "iat": testNow.Unix()}) // no exp: the end comes from iat
	f.nextAccess = f.access
	f.refreshCookie = "refresh-login"
	s, c := clockStore(t, f)
	ctx := passwordCtx()
	calls(t, s, ctx, 1)
	c.advance(time.Hour) // past the end: a renewal
	calls(t, s, ctx, 1)
	out := buf.String()
	for _, want := range []string{"source=iat", "claims=iat,sub", "cookies=AWSALB,AWSALBCORS,access_token,api_session,refresh_token", "status=200"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{f.access, "refresh-login", "api-login", "lb-login", "lbc-login"} {
		if strings.Contains(out, secret) {
			t.Errorf("a value reached the log: %q", secret)
		}
	}
}

func TestCookieEndReadsMaxAgeOrExpires(t *testing.T) {
	now := testNow
	set := func(v string) http.Header { return http.Header{"Set-Cookie": {v}} }
	if got := cookieEnd(set("access_token=x; Path=/; Max-Age=600"), now); !got.Equal(now.Add(10 * time.Minute)) {
		t.Errorf("max-age: %v", got)
	}
	later := now.Add(time.Hour).UTC().Format(http.TimeFormat)
	if got := cookieEnd(set("access_token=x; Path=/; Expires="+later), now); !got.Equal(now.Add(time.Hour).Truncate(time.Second)) {
		t.Errorf("expires: %v", got)
	}
	if got := cookieEnd(set("access_token=x; Path=/; Max-Age=0"), now); !got.IsZero() {
		t.Errorf("max-age 0 must give no end: %v", got)
	}
	if got := cookieEnd(set("refresh_token=x; Path=/; Max-Age=600"), now); !got.IsZero() {
		t.Errorf("another cookie gave an end: %v", got)
	}
	if got := cookieEnd(nil, now); !got.IsZero() {
		t.Errorf("no cookie gave an end: %v", got)
	}
}

// The source of the end, in the order the adapter tries them. A token never has no end.
func TestTokenEndSources(t *testing.T) {
	now := testNow
	hour := now.Add(time.Hour).Unix()
	var none time.Time
	cases := []struct {
		name         string
		tok          string
		cookieEnd    time.Time
		savedEnd     time.Time
		want         time.Time
		wantedSource string
	}{
		{"exp as an integer", makeJWT(now.Add(time.Hour), userID), none, none, now.Add(time.Hour), "exp"},
		{"exp as a float", jwtWith(obj{"exp": float64(hour) + 0.75}), none, none, time.Unix(hour, 0), "exp"},
		{"exp as a numeric string", jwtWith(obj{"exp": strconv.FormatInt(hour, 10)}), none, none, time.Unix(hour, 0), "exp"},
		{"no exp, the cookie's end", jwtWith(obj{"sub": userID}), now.Add(10 * time.Minute), none, now.Add(10 * time.Minute), "cookie"},
		{"no exp, a saved end", jwtWith(obj{"sub": userID}), none, now.Add(5 * time.Minute), now.Add(5 * time.Minute), "saved"},
		{"no exp, iat", jwtWith(obj{"sub": userID, "iat": now.Unix()}), none, none, now.Add(fallbackLife), "iat"},
		{"no claims at all", jwtWith(obj{"sub": userID}), none, none, now.Add(fallbackLife), "received"},
		{"not a JWT", "opaque-token", none, none, now.Add(fallbackLife), "received"},
	}
	for _, tc := range cases {
		m, _ := jwtClaims(tc.tok)
		got, src := tokenEnd(m, tc.cookieEnd, tc.savedEnd, now)
		if !got.Equal(tc.want) || src != tc.wantedSource {
			t.Errorf("%s: got %v from %s, want %v from %s", tc.name, got, src, tc.want, tc.wantedSource)
		}
	}
}
