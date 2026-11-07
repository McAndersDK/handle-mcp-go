package livelist

// The read-only guards of the live test. They are copies of the guards in the stores' own
// live_test.go files (nemlig, bilkatogo): a _test.go type cannot be imported from another package,
// and the stores' non-test code is not to change. If a store's guard changes, change it here too.
// The offline test TestGuardsStopWrites proves them.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// errorer is what the guards need of a test: they report to it.
type errorer interface {
	Errorf(format string, args ...any)
}

// recorder collects what a guard reports, for the offline test of the guards.
type recorder struct{ reports []string }

func (r *recorder) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

// noWrites is the store with SetQuantities made to fail.
type noWrites struct {
	store.Store
	tb errorer
}

func (n noWrites) SetQuantities(context.Context, []store.CartItem) ([]model.QuantityResult, error) {
	n.tb.Errorf("a live test must never write to the cart")
	return nil, errors.New("live tests never write to the cart")
}

// nemligGuard refuses every request that could change something at nemlig: only GET, and the
// login POST, which sets the login cookie and nothing in the cart.
type nemligGuard struct {
	tb    errorer
	inner http.RoundTripper
}

func (r nemligGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && !(req.Method == http.MethodPost && req.URL.Path == "/webapi/login") {
		r.tb.Errorf("a live test must only read: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("live tests only read")
	}
	return r.inner.RoundTrip(req)
}

// bilkaGuard refuses everything but GET (not ChangeLineCount, which is a GET that writes), the
// login POSTs, the Algolia search POST and the order list POST, which only reads.
type bilkaGuard struct {
	tb    errorer
	inner http.RoundTripper
}

func (r bilkaGuard) allowed(req *http.Request) bool {
	path := req.URL.Path
	if strings.Contains(path, "ChangeLineCount") {
		return false
	}
	switch req.Method {
	case http.MethodGet:
		return true
	case http.MethodPost:
		switch {
		case req.URL.Host == "accounts.eu1.gigya.com":
			return path == "/accounts.login" || path == "/accounts.getJWT"
		case req.URL.Host == "api.bilkatogo.dk":
			return path == "/api/auth/LoginJWT" || path == "/api/shop/v7/OrderHistory"
		case strings.HasSuffix(req.URL.Host, ".algolia.net"):
			return path == "/1/indexes/*/queries"
		}
	}
	return false
}

func (r bilkaGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if !r.allowed(req) {
		r.tb.Errorf("a live test must only read: %s %s", req.Method, req.URL.Path)
		return nil, errors.New("live tests only read")
	}
	return r.inner.RoundTrip(req)
}
