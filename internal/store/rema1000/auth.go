package rema1000

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

const (
	accessCookie  = "access_token"
	refreshCookie = "refresh_token"
	apiCookie     = "api_session"             // the site's session cookie; the refresh call answers 500 without it
	callbackPath  = "/api/auth/callback/rema" // where the OAuth login ends on the site
	maxHops       = 12
	awsCookie     = "AWSALB"         // the load balancer's cookie: it routes the refresh to the backend that holds api_session
	awsCorsCookie = "AWSALBCORS"     // its CORS twin, rotated with it
	expKey        = "access_exp"     // when the access token ends, Unix seconds, saved with the session
	fallbackLife  = 50 * time.Minute // the life given to a token that says nothing about its end
)

// allowedHosts are the only hosts the login may be sent to. Anything else, queue-it above all,
// stops the login: a queue or a bot check is never worked around.
var allowedHosts = map[string]bool{shopHost: true, loginHost: true, appHost: true, apiHost: true}

// keptCookies are the cookies of the shop's session: saved, restored into the jar and read back
// after every answer from the shop. The api host gets none of them (it takes the Bearer token only).
var keptCookies = []string{accessCookie, refreshCookie, apiCookie, awsCookie, awsCorsCookie}

// acquire returns the Bearer token to send. It logs in or renews when there is none, it is about
// to end, or `force` says the last attempt got a 401 (and `used` is the token that attempt had,
// so a renewal another call has just made is not repeated).
func (s *Store) acquire(ctx context.Context, force bool, used string) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.mu.Lock()
	tok, exp, hasRefresh := s.token, s.tokenExp, s.cookies[refreshCookie] != ""
	s.mu.Unlock()
	if tok != "" && !(force && tok == used) && s.now().Add(tokenMargin).Before(exp) {
		return tok, nil
	}
	login, err := store.LoadLogin(ctx, name)
	if err != nil {
		return "", err
	}
	if tok == "" && login.Session != nil { // first use: the session saved by `handle-mcp login`
		if err := s.adopt(login); err != nil {
			return "", err
		}
		s.mu.Lock()
		tok, exp, hasRefresh = s.token, s.tokenExp, s.cookies[refreshCookie] != ""
		s.mu.Unlock()
		if !force && s.now().Add(tokenMargin).Before(exp) {
			return tok, nil
		}
	}
	if tok == "" && login.Password != "" {
		return s.fullLogin(ctx, login)
	}
	if hasRefresh {
		// Why a renewal happens, never the token: a fresh token should not get here.
		slog.Info("REMA 1000 renewing the session", "forced", force, "has_token", tok != "", "has_exp", !exp.IsZero(), "expired", !exp.IsZero() && !s.now().Add(tokenMargin).Before(exp))
		next, err := s.renew(ctx, login)
		if err == nil {
			return next, nil
		}
		if !errors.Is(err, errRefused) {
			return "", err
		}
	}
	if login.Password != "" { // an env login can start over
		return s.fullLogin(ctx, login)
	}
	return "", store.Expired(name)
}

// adopt puts a saved session into the cookie jar and the store's state.
func (s *Store) adopt(login store.Login) error {
	c := sessionCookies(login.Session)
	if c[accessCookie] == "" && c[refreshCookie] == "" {
		return store.Expired(name)
	}
	s.jar.SetCookies(shopURL, cookiesOf(c))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savedStore = savedIDs(login.Session)
	s.setCookiesLocked(c, time.Time{}, savedExp(login.Session))
	return nil
}

// savedExp is access_exp of a saved session: when the access token ends. Zero for an old session.
func savedExp(session map[string]string) time.Time {
	sec, err := strconv.ParseInt(strings.TrimSpace(session[expKey]), 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// sessionCookies is the kept cookies of a saved session, without empty values.
func sessionCookies(session map[string]string) map[string]string {
	out := map[string]string{}
	for _, n := range keptCookies {
		if v := session[n]; v != "" {
			out[n] = v
		}
	}
	return out
}

// jarCookies reads the kept cookies back from the jar. The site may have rotated any of them.
func (s *Store) jarCookies() map[string]string {
	out := map[string]string{}
	for _, n := range keptCookies {
		if v := s.cookie(n); v != "" {
			out[n] = v
		}
	}
	return out
}

func cookiesOf(c map[string]string) []*http.Cookie {
	var out []*http.Cookie
	for _, n := range keptCookies {
		if c[n] != "" {
			out = append(out, &http.Cookie{Name: n, Value: c[n], Path: "/", Secure: true})
		}
	}
	return out
}

// setCookiesLocked keeps the raw cookie values (c, the kept cookies) and reads the Bearer token and
// its end from the access cookie. cookieEnd is when the access cookie of the answer that carried it
// ends (zero when the answer did not say); savedEnd is access_exp of a restored session (zero when
// there is none). An access token that is not a JWT is treated as ended, so it is renewed.
func (s *Store) setCookiesLocked(c map[string]string, cookieEnd, savedEnd time.Time) {
	s.cookies = map[string]string{}
	for _, n := range keptCookies {
		if c[n] != "" {
			s.cookies[n] = c[n]
		}
	}
	s.token, s.tokenExp = "", time.Time{}
	access := c[accessCookie]
	if access == "" {
		return
	}
	tok := access
	if u, err := url.QueryUnescape(access); err == nil {
		tok = u
	}
	s.token = tok
	m, _ := jwtClaims(tok)
	var src string
	s.tokenExp, src = tokenEnd(m, cookieEnd, savedEnd, s.now())
	if src != "saved" {
		slog.Info("REMA 1000 token end found", "source", src, "claims", claimNames(m))
	}
	if _, sub := claims(tok); sub != 0 {
		s.userID = sub
	}
}

// tokenEnd says when the access token ends and which source said so. The JWT's exp comes first.
// Then the access cookie's Max-Age or Expires from the answer, then a saved access_exp, then iat
// plus fallbackLife, and last the time the token was received plus fallbackLife. A token never
// has no end: "no end" would renew it on every call.
func tokenEnd(m map[string]any, cookieEnd, savedEnd, now time.Time) (time.Time, string) {
	if t, ok := claimTime(m, "exp"); ok {
		return t, "exp"
	}
	if !cookieEnd.IsZero() {
		return cookieEnd, "cookie"
	}
	if !savedEnd.IsZero() {
		return savedEnd, "saved"
	}
	if t, ok := claimTime(m, "iat"); ok {
		return t.Add(fallbackLife), "iat"
	}
	return now.Add(fallbackLife), "received"
}

// cookieEnd is when the access cookie of an answer ends: its Max-Age, else its Expires. Zero when
// the answer does not say, or says it has already ended.
func cookieEnd(h http.Header, now time.Time) time.Time {
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name != accessCookie {
			continue
		}
		switch {
		case c.MaxAge > 0:
			return now.Add(time.Duration(c.MaxAge) * time.Second)
		case c.MaxAge == 0 && c.Expires.After(now):
			return c.Expires
		}
	}
	return time.Time{}
}

// jwtClaims reads the payload of a JWT without checking it. ok is false when tok is not one.
func jwtClaims(tok string) (m map[string]any, ok bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, false
	}
	seg := strings.TrimRight(parts[1], "=")
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(seg)
	}
	if err != nil {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&m) != nil {
		return nil, false
	}
	return m, true
}

// claims reads exp and sub from a JWT without checking it; zero values when it is not one.
func claims(tok string) (time.Time, int64) {
	m, _ := jwtClaims(tok)
	exp, _ := claimTime(m, "exp")
	sub, _ := numeric(m["sub"])
	return exp, sub
}

// claimTime is a time claim (seconds since 1970), or false.
func claimTime(m map[string]any, key string) (time.Time, bool) {
	sec, ok := numeric(m[key])
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// numeric reads a claim that is a JSON number (integer or float, truncated to whole seconds) or a
// numeric string. Anything else, or a value that is not a plausible time, is false.
func numeric(v any) (int64, bool) {
	var f float64
	var err error
	switch t := v.(type) {
	case json.Number:
		f, err = t.Float64()
	case string:
		f, err = strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return 0, false
	}
	if err != nil || !(f > 0) || math.IsInf(f, 0) || f > 1e12 {
		return 0, false
	}
	return int64(f), true
}

// claimNames lists the keys of a JWT payload, sorted. Never the values.
func claimNames(m map[string]any) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// cookieNames lists the names of cookies, sorted. Never the values.
func cookieNames(cs []*http.Cookie) string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// user is the user id behind the token, for the shopping-list calls.
func (s *Store) user() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userID == 0 {
		return 0, store.Errorf("REMA 1000: login gav intet bruger-id. API'et kan have ændret sig.")
	}
	return s.userID, nil
}

// renew trades the cookies for a new access token (POST /api/auth/refresh on the site, the same
// call the webshop makes). The call carries the kept cookies: the login cookies and the load
// balancer's AWSALB pair, which routes it to the backend that holds api_session. REMA answers with
// an empty body and sets the cookies again, so they are read back from the jar. A 4xx is
// errRefused, and so is any refusal of a session without the whole set (an old session: log in
// again); a 5xx on a complete session is a store error. The call is never retried: a retried
// refresh could burn a rotated refresh token. The session is saved again when any cookie changed.
func (s *Store) renew(ctx context.Context, login store.Login) (string, error) {
	s.mu.Lock()
	complete := s.cookies[apiCookie] != "" && s.cookies[awsCookie] != ""
	s.mu.Unlock()
	h := apiHeaders("")
	h.Set("Origin", shopBase)
	sent := cookieNames(s.jar.Cookies(shopURL))
	resp, err := s.sender.Send(ctx, store.Request{Method: "POST", URL: shopBase + "/api/auth/refresh", Header: h, NoRetry: true})
	if err != nil {
		return "", err
	}
	slog.Info("REMA 1000 renewal answered", "cookies", sent, "status", resp.Status)
	if resp.Status >= 300 {
		slog.Info("REMA 1000 session could not be renewed", "status", resp.Status)
		if resp.Status >= 500 && complete {
			return "", store.Errorf("REMA 1000 svarer %d ved fornyelse. Prøv igen senere.", resp.Status)
		}
		return "", errRefused
	}
	c := s.jarCookies()
	if c[accessCookie] == "" {
		c[accessCookie] = tokenInBody(resp.Body) // the fallback, used only when the answer has a token
	}
	if c[accessCookie] == "" {
		return "", errRefused
	}
	s.mu.Lock()
	for _, n := range keptCookies { // a cookie the answer did not send keeps its value
		if c[n] == "" {
			c[n] = s.cookies[n]
		}
	}
	old, oldExp := s.cookies, s.tokenExp
	s.setCookiesLocked(c, cookieEnd(resp.Header, s.now()), time.Time{})
	changed := !sameCookies(old, s.cookies) || s.tokenExp.Unix() != oldExp.Unix()
	tok, session := s.token, s.sessionLocked()
	s.mu.Unlock()
	if changed {
		store.SaveSession(ctx, name, login.Email, session)
	}
	return tok, nil
}

// sameCookies says whether two sets of kept cookies hold the same values.
func sameCookies(a, b map[string]string) bool {
	for _, n := range keptCookies {
		if a[n] != b[n] {
			return false
		}
	}
	return true
}

// tokenInBody is the access token of a JSON answer ({"tokens":{"access_token":...}}), or "".
func tokenInBody(b []byte) string {
	var body struct {
		Tokens struct {
			Access string `json:"access_token"`
		} `json:"tokens"`
	}
	_ = json.Unmarshal(b, &body)
	return body.Tokens.Access
}

func (s *Store) sessionLocked() map[string]string {
	out := map[string]string{}
	for k, v := range s.cookies {
		out[k] = v
	}
	if s.savedStore != "" {
		out[savedKey] = s.savedStore
	}
	if !s.tokenExp.IsZero() {
		out[expKey] = strconv.FormatInt(s.tokenExp.Unix(), 10)
	}
	return out
}

// cookie is the raw value of a cookie the site has set, or "".
func (s *Store) cookie(n string) string {
	for _, c := range s.jar.Cookies(shopURL) {
		if c.Name == n {
			return c.Value
		}
	}
	return ""
}

// fullLogin is the site's own login with email and password. Only the session cookies are kept.
func (s *Store) fullLogin(ctx context.Context, login store.Login) (string, error) {
	page, err := s.follow(ctx, shopBase+"/konto/auth?redirect_uri=%2Fkonto", func(*url.URL) bool { return false })
	if err != nil {
		return "", err
	}
	if page.Status != http.StatusOK || hostOf(page.URL) != loginHost {
		return "", store.Errorf("REMA 1000: loginsiden blev ikke nået. API'et kan have ændret sig.")
	}
	body, err := json.Marshal(map[string]string{"email": login.Email, "password": login.Password})
	if err != nil {
		return "", store.Errorf("REMA 1000: login kunne ikke sendes.")
	}
	h := apiHeaders("")
	h.Set("Content-Type", "application/json")
	h.Set("X-Language", "da")
	h.Set("Origin", "https://"+loginHost)
	h.Set("Referer", page.URL)
	resp, err := s.sender.Send(ctx, store.Request{Method: "POST", URL: "https://" + appHost + "/api/oauth2/login", Header: h, Body: body})
	if err != nil {
		return "", err
	}
	switch {
	case resp.Status == 400 || resp.Status == 401 || resp.Status == 422:
		return "", store.Errorf("REMA 1000 afviser login. Tjek email og kodeord.")
	case resp.Status == 403 || resp.Status >= 300 && resp.Status < 400:
		return "", errBot
	case resp.Status >= 300:
		return "", store.Errorf("REMA 1000 login fejlede (%d).", resp.Status)
	}
	var out struct {
		Redirect string `json:"redirect_uri"`
	}
	if json.Unmarshal(resp.Body, &out) != nil || out.Redirect == "" {
		return "", store.Errorf("REMA 1000 login svarede uden videre sti. API'et kan have ændret sig.")
	}
	next := strings.Replace(out.Redirect, "http://", "https://", 1)
	// Follow the authorize hops; stop once the site callback has answered, because that answer
	// sets the cookies and the rest of the chain is only the webshop's own page.
	cb, err := s.follow(ctx, next, func(u *url.URL) bool { return u.Hostname() == shopHost && u.Path == callbackPath })
	if err != nil {
		return "", err
	}
	c := s.jarCookies()
	if c[accessCookie] == "" {
		return "", store.Errorf("REMA 1000 login gav ingen adgangsnøgle. API'et kan have ændret sig.")
	}
	s.mu.Lock()
	s.setCookiesLocked(c, cookieEnd(cb.Header, s.now()), time.Time{})
	tok, session := s.token, s.sessionLocked()
	s.mu.Unlock()
	slog.Info("logged in to REMA 1000")
	if login.Password != "" { // an old saved password is replaced by the session
		store.SaveSession(ctx, name, login.Email, session)
	}
	return tok, nil
}

// follow GETs a page and follows its redirects by hand, one allowed host at a time, so cookies
// land in the jar and nothing goes anywhere unexpected. stop says whether the page just fetched
// is the last one. It returns the last answer.
func (s *Store) follow(ctx context.Context, start string, stop func(*url.URL) bool) (*store.Response, error) {
	h := http.Header{"Accept": {"text/html"}}
	cur := start
	for hop := 0; hop < maxHops; hop++ {
		u, err := url.Parse(cur)
		if err != nil || u.Scheme != "https" || !allowedHosts[strings.ToLower(u.Hostname())] {
			if err == nil && queueIt(u.Hostname()) {
				return nil, errQueue
			}
			return nil, store.Errorf("REMA 1000 sendte login et uventet sted hen. API'et kan have ændret sig.")
		}
		resp, err := s.sender.Send(ctx, store.Request{Method: "GET", URL: cur, Header: h})
		if err != nil {
			return nil, err
		}
		resp.URL = cur
		loc := resp.Header.Get("Location")
		if stop(u) || resp.Status < 300 || resp.Status >= 400 || loc == "" {
			if resp.Status >= 400 {
				return nil, store.Errorf("REMA 1000 login fejlede (%d).", resp.Status)
			}
			return resp, nil
		}
		next, err := u.Parse(loc)
		if err != nil {
			return nil, store.Errorf("REMA 1000 sendte login et uventet sted hen. API'et kan have ændret sig.")
		}
		cur = next.String()
	}
	return nil, store.Errorf("REMA 1000 login gik i ring. API'et kan have ændret sig.")
}

func queueIt(host string) bool {
	host = strings.ToLower(host)
	return host == "queue-it.net" || strings.HasSuffix(host, ".queue-it.net")
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// apiHeaders are the headers the webshop's own API client sends.
func apiHeaders(token string) http.Header {
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("X-Device", "web")
	h.Set("X-Locale", "da")
	h.Set("X-Timezone", "Copenhagen/Europe")
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h
}
