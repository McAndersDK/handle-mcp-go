package bilkatogo

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"net/http/cookiejar"
)

// cartCookie is the name of the cookie LoginJWT must never receive: it carries an anonymous cart,
// and the login would REPLACE the account's cart with it (docs/research.md).
const cartCookie = "cart"

// cartJar is a cookie jar that can forget the `cart` cookie, as httpx's cookies.delete("cart")
// did. Go's cookiejar cannot list or delete by name, so the jar remembers where it was set and
// expires it there.
type cartJar struct {
	mu    sync.Mutex
	inner *cookiejar.Jar
	where []setAt
}

type setAt struct {
	u            *url.URL
	domain, path string
}

func newCartJar() *cartJar {
	inner, _ := cookiejar.New(nil) // New(nil) never fails
	return &cartJar{inner: inner}
}

func (j *cartJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range cookies {
		if c.Name == cartCookie {
			cp := *u
			j.where = append(j.where, setAt{&cp, c.Domain, c.Path})
		}
	}
	j.inner.SetCookies(u, cookies)
}

func (j *cartJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.inner.Cookies(u)
}

// forgetCart expires every `cart` cookie the jar has been given.
func (j *cartJar) forgetCart() {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, w := range j.where {
		j.inner.SetCookies(w.u, []*http.Cookie{{Name: cartCookie, Domain: w.domain, Path: w.path, MaxAge: -1}})
	}
	j.where = nil
}

type noCartKey struct{}

// withoutCart marks a request that must not carry the `cart` cookie, whatever the jar holds. A
// call that is still in flight when a login runs may set the cookie again at any moment, so
// forgetting it is not enough.
func withoutCart(ctx context.Context) context.Context {
	return context.WithValue(ctx, noCartKey{}, true)
}

// stripCart is the transport that keeps that promise.
type stripCart struct{ inner http.RoundTripper }

func (t stripCart) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context().Value(noCartKey{}) == nil {
		return t.inner.RoundTrip(req)
	}
	cp := req.Clone(req.Context()) // a RoundTripper must not change its request
	cp.Header.Del("Cookie")
	for _, c := range req.Cookies() {
		if c.Name != cartCookie {
			cp.AddCookie(c)
		}
	}
	return t.inner.RoundTrip(cp)
}
