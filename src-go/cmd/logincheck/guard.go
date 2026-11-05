package main

import (
	"net/http"
	"strings"
)

// rule is one allowed call: exact method, host and path. Query strings are not compared.
type rule struct{ method, host, path string }

const (
	nemligHost  = "www.nemlig.com"
	searchHost  = "webapi.prod.knl.nemlig.it"
	gigyaHost   = "accounts.eu1.gigya.com"
	bilkaHost   = "api.bilkatogo.dk"
	algoliaHost = "f9vbjlr1bk-dsn.algolia.net"
	loginJWT    = "/api/auth/LoginJWT"
)

// allowlist is every call this program may make. Nothing else, no AddToBasket.
var allowlist = []rule{
	{"GET", nemligHost, "/webapi/Token"},
	{"GET", nemligHost, "/webapi/AntiForgery"},
	{"POST", nemligHost, "/webapi/login"},
	{"GET", nemligHost, "/webapi/basket/GetBasket"},
	{"GET", nemligHost, "/webapi/v2/AppSettings/Website"},
	{"GET", searchHost, "/searchgateway/api/search"},
	{"POST", gigyaHost, "/accounts.login"},
	{"POST", gigyaHost, "/accounts.getJWT"},
	{"POST", bilkaHost, loginJWT},
	{"GET", bilkaHost, "/api/shop/v6/Cart"},
	{"POST", algoliaHost, "/1/indexes/*/queries"},
}

// blockedError is returned by the guard; the step runner turns it into BLOCKED <step>.
type blockedError struct{ reason string }

func (e *blockedError) Error() string { return "blocked: " + e.reason }

func queueIt(host string) bool {
	host = strings.ToLower(host)
	return host == "queue-it.net" || strings.HasSuffix(host, ".queue-it.net")
}

// check returns nil when the request is on the allowlist.
func check(req *http.Request) error {
	u := req.URL
	if queueIt(u.Hostname()) {
		return &blockedError{"queue_it"}
	}
	if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.User != nil || u.Opaque != "" {
		return &blockedError{"not_allowed"}
	}
	if u.RawPath != "" && u.RawPath != u.Path {
		return &blockedError{"not_allowed"}
	}
	host := strings.ToLower(u.Hostname())
	ok := false
	for _, r := range allowlist {
		if r.method == req.Method && r.host == host && r.path == u.Path {
			ok = true
			break
		}
	}
	if !ok {
		return &blockedError{"not_allowed"}
	}
	// Bilka: LoginJWT replaces the account's cart with the one behind a `cart` cookie.
	if host == bilkaHost && u.Path == loginJWT {
		for _, c := range req.Cookies() {
			if strings.EqualFold(c.Name, "cart") {
				return &blockedError{"cart_cookie"}
			}
		}
	}
	return nil
}

// guard checks every request, including every redirect hop, before it leaves the program.
type guard struct{ inner http.RoundTripper }

func (g guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := check(req); err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}
	resp, err := g.inner.RoundTrip(req)
	if resp != nil && resp.Request == nil {
		resp.Request = req
	}
	return resp, err
}
