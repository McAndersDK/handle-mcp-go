package livelist

// The per-item report (port of the print loop in test_fixed_list_live) and its offline tests.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/McAndersDK/handle-mcp-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// --- the report ----------------------------------------------------------------------------------

// priceList prices the entries in one store with compare.Quote, like compare_basket does for one
// store, and reports per item through logf. It returns the failure message ("" when none fit).
func priceList(ctx context.Context, name string, s store.Store, entries []Entry, logf func(string, ...any)) (string, error) {
	items := make([]string, len(entries))
	for i, e := range entries {
		items[i] = e.Item
	}
	result, err := compare.Quote(ctx, s, items)
	if err != nil {
		return "", err
	}
	byItem := map[string]*compare.BasketLine{}
	for i := range result.Lines {
		byItem[result.Lines[i].Item] = &result.Lines[i]
	}
	var failed []string
	warned := 0
	logf("== %s: %d varer, %s kr. ==", name, len(entries), result.ProductsTotal)
	for _, e := range entries {
		line := byItem[e.Item]
		fail, warn := check(e, line)
		if len(warn) > 0 {
			warned++
		}
		mark := "  "
		switch {
		case len(fail) > 0:
			mark = "X "
		case len(warn) > 0:
			mark = "! "
		case line != nil && !line.Sure:
			mark = "? "
		}
		pick := "-"
		note := ""
		if line != nil && line.Product != nil {
			pick = padRight(truncate(line.Product.Name, 36), 36) + " " + padLeft(line.LineTotal.String(), 7)
			note = deref(line.Note)
		}
		detail := strings.Join(append(append([]string{}, fail...), warn...), "; ")
		if detail == "" {
			detail = note
		}
		logf("%s%s -> %s %s", mark, padRight(e.Item, 34), pick, detail)
		if len(fail) > 0 {
			msg := e.Item + ": " + strings.Join(fail, "; ")
			if e.Why != "" {
				msg += " (" + e.Why + ")"
			}
			failed = append(failed, msg)
		}
	}
	logf("%d forslag med advarsel (modellen kan vælge en anden kandidat)", warned)
	if len(failed) == 0 {
		return "", nil
	}
	return fmt.Sprintf("%s: %d varer uden passende kandidat:\n%s", name, len(failed), strings.Join(failed, "\n")), nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func padRight(s string, n int) string {
	if c := utf8.RuneCountInString(s); c < n {
		return s + strings.Repeat(" ", n-c)
	}
	return s
}

func padLeft(s string, n int) string {
	if c := utf8.RuneCountInString(s); c < n {
		return strings.Repeat(" ", n-c) + s
	}
	return s
}

// --- offline: the guards and the report ------------------------------------------------------------

type okTransport struct{}

func (okTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func request(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// The guards themselves are tested offline: a write must fail, whatever the test does.
func TestGuardsStopWrites(t *testing.T) {
	probe := &recorder{}
	n := nemligGuard{tb: probe, inner: okTransport{}}
	b := bilkaGuard{tb: probe, inner: okTransport{}}
	r := remaGuard{tb: probe, inner: okTransport{}}
	type call struct {
		rt          http.RoundTripper
		method, url string
		allowed     bool
	}
	calls := []call{
		{r, "POST", "https://api.digital.rema1000.dk/api/v2/shoppinglists", false},
		{r, "PATCH", "https://api.digital.rema1000.dk/api/v2/shoppinglists/1", false},
		{r, "GET", "https://api.digital.rema1000.dk/api/v3/stores", true},
		{r, "POST", "https://cphapp.rema1000.dk/api/oauth2/login", true},
		{r, "POST", "https://shop.rema1000.dk/api/auth/refresh", true},
		{n, "POST", "https://www.nemlig.com/webapi/basket/AddToBasket", false},
		{n, "PUT", "https://www.nemlig.com/webapi/x", false},
		{n, "DELETE", "https://www.nemlig.com/webapi/x", false},
		{n, "GET", "https://www.nemlig.com/webapi/Token", true},
		{n, "POST", "https://www.nemlig.com/webapi/login", true},
		{b, "GET", "https://api.bilkatogo.dk/api/shop/v6/ChangeLineCount?productId=1&count=1", false},
		{b, "POST", "https://api.bilkatogo.dk/api/shop/v6/ChangeLineCount", false},
		{b, "POST", "https://api.bilkatogo.dk/api/shop/v6/Cart", false},
		{b, "PUT", "https://api.bilkatogo.dk/api/shop/v6/Cart", false},
		{b, "DELETE", "https://api.bilkatogo.dk/api/shop/v6/Cart", false},
		{b, "POST", "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/other", false},
		{b, "POST", "https://accounts.eu1.gigya.com/accounts.setAccountInfo", false},
		{b, "GET", "https://api.bilkatogo.dk/api/shop/v6/Cart", true},
		{b, "POST", "https://accounts.eu1.gigya.com/accounts.login", true},
		{b, "POST", "https://accounts.eu1.gigya.com/accounts.getJWT", true},
		{b, "POST", "https://api.bilkatogo.dk/api/auth/LoginJWT", true},
		{b, "POST", "https://api.bilkatogo.dk/api/shop/v7/OrderHistory", true},
		{b, "POST", "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/*/queries", true},
	}
	writes := 0
	for _, c := range calls {
		_, err := c.rt.RoundTrip(request(t, c.method, c.url))
		if c.allowed && err != nil {
			t.Errorf("%s %s was stopped: %v", c.method, c.url, err)
		}
		if !c.allowed {
			writes++
			if err == nil {
				t.Errorf("%s %s got through the live guard", c.method, c.url)
			}
		}
	}
	wrapped := noWrites{tb: probe}
	if _, err := wrapped.SetQuantities(context.Background(), []store.CartItem{{ProductID: "1", Quantity: 1}}); err == nil {
		t.Error("SetQuantities must fail in a live test")
	}
	if want := writes + 1; len(probe.reports) != want {
		t.Errorf("the guards must report every write once: %d reports, want %d: %v", len(probe.reports), want, probe.reports)
	}
}

// fakeStore answers the three reads compare.Quote makes; every other method would panic on the nil
// embedded Store, which proves the report needs nothing else.
type fakeStore struct {
	store.Store
	products []model.Product
}

func (f fakeStore) Search(context.Context, string, int) ([]model.Product, error) {
	return f.products, nil
}

func (f fakeStore) DeliveryInfo(context.Context) (model.DeliveryInfo, error) {
	return model.DeliveryInfo{}, store.ErrNotImplemented
}

func (f fakeStore) Terms(context.Context) (model.StoreTerms, error) {
	t := model.NewStoreTerms("fake", false)
	mode := model.DeliveryPickup
	t.DeliveryMode = &mode
	pickup := "Butik i Testby, Testvej 1" // must never reach the report
	t.PickupStore = &pickup
	return t, nil
}

func TestReportFailsAnItemWithNoFittingCandidateAndHidesThePickupStore(t *testing.T) {
	fake := fakeStore{products: []model.Product{
		model.NewProduct("fake", "1", "Hvidløg 3 stk", money(t, "12")),
		model.NewProduct("fake", "2", "Havregryn 1 kg", money(t, "15")),
	}}
	entries := []Entry{
		{Item: "hvidløg", Has: []string{"hvidløg"}},
		{Item: "leverpostej", Has: []string{"leverpostej"}, Why: "a reason"},
	}
	var out []string
	logf := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	msg, err := priceList(context.Background(), "fake", fake, entries, logf)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(out, "\n")
	if !strings.Contains(msg, "fake: 1 varer uden passende kandidat:\nleverpostej: ") || !strings.Contains(msg, "(a reason)") {
		t.Errorf("failure message: %q\nreport:\n%s", msg, all)
	}
	if strings.Contains(msg, "hvidløg:") {
		t.Errorf("hvidløg should fit: %q", msg)
	}
	if !strings.Contains(all, "== fake: 2 varer,") || !strings.Contains(all, "Hvidløg 3 stk") || !strings.Contains(all, "X leverpostej") {
		t.Errorf("report:\n%s", all)
	}
	if strings.Contains(all, "Testby") || strings.Contains(all, "Testvej") {
		t.Errorf("the pickup store leaked into the report:\n%s", all)
	}
}

func TestReportKeepsAGapBeforeAPriceUnderTen(t *testing.T) {
	fake := fakeStore{products: []model.Product{
		model.NewProduct("fake", "1", "MØRKT RUGBRØD", money(t, "5.95")),
		model.NewProduct("fake", "2", "GÆR", money(t, "1.5")),
	}}
	entries := []Entry{{Item: "rugbrød", Has: []string{"rugbrød"}}, {Item: "gær", Has: []string{"gær"}}}
	var out []string
	logf := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if _, err := priceList(context.Background(), "fake", fake, entries, logf); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(out, "\n")
	for _, bad := range []string{"RUGBRØD5.95", "GÆR1.50"} {
		if strings.Contains(all, bad) {
			t.Errorf("no gap before the price (%s):\n%s", bad, all)
		}
	}
	if !strings.Contains(all, " 5.95") || !strings.Contains(all, " 1.50") {
		t.Errorf("prices should have two decimals:\n%s", all)
	}
}
