package rema1000

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
)

// allowedHosts are the only hosts the login may be sent to. Anything else, queue-it above all,
// stops the login: a queue or a bot check is never worked around.
var allowedHosts = map[string]bool{shopHost: true, loginHost: true, appHost: true, apiHost: true}

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
	access, refresh, api := login.Session[accessCookie], login.Session[refreshCookie], login.Session[apiCookie]
	if access == "" && refresh == "" {
		return store.Expired(name)
	}
	s.jar.SetCookies(shopURL, cookiesOf(map[string]string{accessCookie: access, refreshCookie: refresh, apiCookie: api}))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savedStore = savedIDs(login.Session)
	s.setCookiesLocked(access, refresh, api)
	return nil
}

func cookiesOf(m map[string]string) []*http.Cookie {
	var out []*http.Cookie
	for _, k := range []string{accessCookie, refreshCookie, apiCookie} {
		if m[k] != "" {
			out = append(out, &http.Cookie{Name: k, Value: m[k], Path: "/", Secure: true})
		}
	}
	return out
}

// setCookiesLocked keeps the raw cookie values and reads the Bearer token and its end from the
// access cookie. An access token that is not a JWT is treated as ended, so it is renewed.
func (s *Store) setCookiesLocked(access, refresh, api string) {
	s.cookies = map[string]string{}
	if access != "" {
		s.cookies[accessCookie] = access
	}
	if refresh != "" {
		s.cookies[refreshCookie] = refresh
	}
	if api != "" {
		s.cookies[apiCookie] = api
	}
	s.token, s.tokenExp = "", time.Time{}
	if access == "" {
		return
	}
	tok := access
	if u, err := url.QueryUnescape(access); err == nil {
		tok = u
	}
	s.token = tok
	exp, sub := claims(tok)
	s.tokenExp = exp
	if sub != 0 {
		s.userID = sub
	}
}

// claims reads exp and sub from a JWT without checking it; zero values when it is not one.
func claims(tok string) (time.Time, int64) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return time.Time{}, 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, 0
	}
	var c struct {
		Exp json.Number `json:"exp"`
		Sub json.Number `json:"sub"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return time.Time{}, 0
	}
	exp, _ := strconv.ParseInt(string(c.Exp), 10, 64)
	sub, _ := strconv.ParseInt(string(c.Sub), 10, 64)
	if exp == 0 {
		return time.Time{}, sub
	}
	return time.Unix(exp, 0), sub
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
// call the webshop makes). The call carries all three cookies. REMA answers with an empty body
// and sets the cookies again, so they are read back from the jar. A 4xx is errRefused, and so is
// any refusal of a session without api_session; a 5xx on a session that has one is a store error.
// The call is never retried: a retried refresh could burn a rotated refresh token. The session is
// saved again when any of the cookies changed.
func (s *Store) renew(ctx context.Context, login store.Login) (string, error) {
	s.mu.Lock()
	hadAPI := s.cookies[apiCookie] != ""
	s.mu.Unlock()
	h := apiHeaders("")
	h.Set("Origin", shopBase)
	resp, err := s.sender.Send(ctx, store.Request{Method: "POST", URL: shopBase + "/api/auth/refresh", Header: h, NoRetry: true})
	if err != nil {
		return "", err
	}
	if resp.Status >= 300 {
		slog.Info("REMA 1000 session could not be renewed", "status", resp.Status)
		if resp.Status >= 500 && hadAPI {
			return "", store.Errorf("REMA 1000 svarer %d ved fornyelse. Prøv igen senere.", resp.Status)
		}
		return "", errRefused
	}
	access, refresh, api := s.cookie(accessCookie), s.cookie(refreshCookie), s.cookie(apiCookie)
	if access == "" {
		access = tokenInBody(resp.Body) // the fallback, used only when the answer has a token
	}
	if access == "" {
		return "", errRefused
	}
	s.mu.Lock()
	if refresh == "" {
		refresh = s.cookies[refreshCookie]
	}
	if api == "" {
		api = s.cookies[apiCookie]
	}
	changed := s.cookies[accessCookie] != access || s.cookies[refreshCookie] != refresh || s.cookies[apiCookie] != api
	s.setCookiesLocked(access, refresh, api)
	tok, session := s.token, s.sessionLocked()
	s.mu.Unlock()
	if changed {
		store.SaveSession(ctx, name, login.Email, session)
	}
	return tok, nil
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
	_, err = s.follow(ctx, next, func(u *url.URL) bool { return u.Hostname() == shopHost && u.Path == callbackPath })
	if err != nil {
		return "", err
	}
	access, refresh, api := s.cookie(accessCookie), s.cookie(refreshCookie), s.cookie(apiCookie)
	if access == "" {
		return "", store.Errorf("REMA 1000 login gav ingen adgangsnøgle. API'et kan have ændret sig.")
	}
	s.mu.Lock()
	s.setCookiesLocked(access, refresh, api)
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
