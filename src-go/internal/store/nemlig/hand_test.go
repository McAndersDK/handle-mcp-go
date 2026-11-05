package nemlig

// Hand tests: Go ports of the tests in tests/test_nemlig.py and tests/test_nemlig_recipes.py that
// the golden files do not already cover, and tests for what Go adds (the traps of the port).
// The list of which Python test lives where is in the report of the port.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// envLogin is a store that logs in with the HANDLE_NEMLIG_ env vars, as the Python tests did.
func envLogin(t *testing.T) (*harness, *Store) {
	t.Helper()
	isolate(t)
	t.Setenv("HANDLE_NEMLIG_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_NEMLIG_PASSWORD", "hemmeligt")
	return newHarness(t)
}

func wantStoreError(t *testing.T, err error, msg string) {
	t.Helper()
	var se *store.StoreError
	if !errors.As(err, &se) {
		t.Fatalf("want a StoreError containing %q, got %v", msg, err)
	}
	if !strings.Contains(se.Msg, msg) {
		t.Fatalf("StoreError %q does not contain %q", se.Msg, msg)
	}
}

// test_env_vars_are_never_saved
func TestEnvLoginIsNeverSaved(t *testing.T) {
	_, s := envLogin(t)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Session(); got[authCookie] != "auth-1" || len(got) != 1 {
		t.Errorf("session %v", got)
	}
	if _, err := keyring.Get(store.KeychainService, name); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("the keychain should be empty, got %v", err)
	}
}

// test_never_calls_order_or_payment_endpoints, stronger: only these calls are ever made, and
// only the login and AddToBasket are not GETs.
func TestOnlyKnownEndpoints(t *testing.T) {
	h, s := envLogin(t)
	ctx := context.Background()
	four := 4
	_, _ = s.Search(ctx, "mælk", 10)
	_, _ = s.Offers(ctx, 10)
	_, _ = s.Recipes(ctx, "kylling", 3)
	_, _ = s.Recipe(ctx, "kebabfad-med-fladbrod-98001303", &four)
	_, _ = s.FeaturedRecipes(ctx, 5)
	_, _ = s.RecipesByTag(ctx, []string{"Familievenlig"}, nil, 5)
	_, _ = s.GetCart(ctx)
	_, _ = s.SetQuantities(ctx, []store.CartItem{{ProductID: "1", Quantity: 1}})
	_, _ = s.Terms(ctx)
	_, _ = s.DeliveryInfo(ctx)
	_, _ = s.DeliverySlots(ctx, 7)
	_, _ = s.Orders(ctx, 5)
	_, _ = s.Order(ctx, "900001")

	exact := map[string]bool{
		"GET /webapi/Token": true, "GET /webapi/AntiForgery": true, "POST /webapi/login": true,
		"GET /webapi/v2/AppSettings/Website": true, "GET /webapi/basket/GetBasket": true,
		"POST /webapi/basket/AddToBasket": true, "GET /webapi/v2/Delivery/GetDeliveryDays": true,
		"GET /webapi/Order/DeliverySpot": true, "GET /webapi/order/GetBasicOrderHistory": true,
		"GET /searchgateway/api/search": true, "GET /productbff/api/web/page": true,
		"GET /productbff/api/web/meals/allRecipes": true, "GET /productbff/api/web/search/recipes": true,
		"GET /productbff/api/web/meals/recipeProductDialog": true,
	}
	prefixes := []string{"GET /webapi/v2/order/GetOrderHistory/", "GET /productbff/api/web/meals/recipeDetails/"}
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if len(h.f.requests) < 20 {
		t.Fatalf("only %d requests were made", len(h.f.requests))
	}
	for _, r := range h.f.requests {
		u, _ := url.Parse(r.URL)
		key := r.Method + " " + u.Path
		ok := exact[key]
		for _, p := range prefixes {
			ok = ok || strings.HasPrefix(key, p)
		}
		if !ok {
			t.Errorf("unexpected call: %s", key)
		}
	}
}

func hit(t *testing.T, text string) obj { return J(t, text).(obj) }

// The product tests of tests/test_nemlig.py (shapes seen live 2026-11-01, docs/research.md).
func TestProductReading(t *testing.T) {
	t.Run("a discount campaign pays the campaign price", func(t *testing.T) {
		p := product(hit(t, `{"Id": 1, "Name": "Laksefilet m. skind", "Price": 120.75, "UnitPriceCalc": 201.25,
		  "Campaign": {"Type": "ProductCampaignDiscountPercent", "DiscountPercent": 30, "CampaignPrice": 84.53,
		  "CampaignUnitPrice": 140.88, "IntervalEnd": "2026-11-04T22:59:59Z"}}`))
		if p.Price.String() != "84.53" || p.UnitPrice.String() != "140.88" || p.BeforePrice.String() != "120.75" {
			t.Errorf("prices %s %s %s", p.Price, p.UnitPrice, p.BeforePrice)
		}
		if !p.OnOffer || *p.OfferText != "-30 %" {
			t.Errorf("offer %v %v", p.OnOffer, p.OfferText)
		}
		if p.OfferUntil.String() != "2026-11-04" { // 22:59:59 UTC is the last minute of the day in DK
			t.Errorf("offer until %s", p.OfferUntil)
		}
	})
	t.Run("the discount label alone is not an offer", func(t *testing.T) {
		p := product(hit(t, `{"Id": 2, "Name": "Letmælk 1,5%", "Price": 10.95, "Campaign": null, "DiscountItem": true}`))
		if p.Price.String() != "10.95" || p.OnOffer || p.OfferText != nil || p.BeforePrice != nil {
			t.Errorf("%+v", p)
		}
	})
	t.Run("multi-buy keeps the single price", func(t *testing.T) {
		p := product(hit(t, `{"Id": 3, "Name": "Hakkede tomater", "Price": 13.95, "Campaign":
		  {"Type": "ProductCampaignMixOffer", "MinQuantity": 4, "TotalPrice": 50.0, "CampaignPrice": 50.0}}`))
		if p.Price.String() != "13.95" || *p.OfferText != "4 for 50 kr" || p.BeforePrice != nil {
			t.Errorf("%s %v %v", p.Price, p.OfferText, p.BeforePrice)
		}
	})
	t.Run("a campaign we cannot describe is still an offer", func(t *testing.T) {
		p := product(hit(t, `{"Id": 4, "Name": "Letmælk", "Price": 21.28,
		  "Campaign": {"Type": "ProductCampaignBuyXForY", "MinQuantity": 3}}`))
		if p.Price.String() != "21.28" || !p.OnOffer || *p.OfferText != "Tilbud" {
			t.Errorf("%+v", p)
		}
	})
	t.Run("a plain discount says the before price", func(t *testing.T) {
		p := product(hit(t, `{"Id": 5, "Name": "Laks - back loin", "Price": 98.75,
		  "Campaign": {"Type": "ProductCampaignDiscount", "CampaignPrice": 79.95}}`))
		if *p.OfferText != "Tilbud, før 98,75 kr" {
			t.Errorf("%v", *p.OfferText)
		}
	})
	t.Run("the multi-buy text is priced by compare", func(t *testing.T) {
		p := product(hit(t, `{"Id": 6, "Name": "Hakkede tomater", "Price": 13.95, "Description": "400 g",
		  "Campaign": {"Type": "ProductCampaignMixOffer", "MinQuantity": 4, "TotalPrice": 50.0}, "Availability": {}}`))
		if got := compare.BestMatch("4 x hakkede tomater 400 g", []model.Product{p}, nil).LineTotal.String(); got != "50.00" {
			t.Errorf("line total %s", got)
		}
	})
	t.Run("the offer ends on a Danish day", func(t *testing.T) {
		p := product(hit(t, `{"Id": 7, "Name": "Is", "Price": 20,
		  "Campaign": {"Type": "ProductCampaignDiscount", "CampaignPrice": 10, "IntervalEnd": "2026-07-01T21:59:59Z"}}`))
		if p.OfferUntil.String() != "2026-07-01" { // 23:59:59 in DK summer time
			t.Errorf("offer until %s", p.OfferUntil)
		}
	})
	t.Run("a free gift costs 0 only because the campaign says so", func(t *testing.T) {
		p := product(hit(t, `{"Id": 8, "Name": "Gave", "Price": 20.45,
		  "Campaign": {"Type": "ProductCampaignFreeProduct", "CampaignPrice": 0, "MaxQuantity": 1}}`))
		if !p.Price.IsZero() || *p.OfferText != "Gratis (max 1 stk.)" || *p.MaxPerOrder != 1 {
			t.Errorf("%+v", p)
		}
	})
}

// A missing price is a format change, never 0 kr (and so never a free product).
func TestMissingPriceIsNotFree(t *testing.T) {
	mustPanicShape := func(name string, f func()) {
		t.Helper()
		defer func() {
			if _, ok := recover().(shapeError); !ok {
				t.Errorf("%s: a missing price must be a shape error", name)
			}
		}()
		f()
	}
	mustPanicShape("hit without Price", func() { product(hit(t, `{"Id": 1, "Name": "x"}`)) })
	mustPanicShape("hit with null Price", func() { product(hit(t, `{"Id": 1, "Name": "x", "Price": null}`)) })
	mustPanicShape("hit with text Price", func() { product(hit(t, `{"Id": 1, "Name": "x", "Price": "NaN"}`)) })
	mustPanicShape("offer without price", func() { offerProduct(hit(t, `{"id": 1, "title": "x"}`), "Dagstilbud") })

	h, s := envLogin(t)
	h.f.respond[pSearch] = []any{jsonResp(200, J(t, `{"Products": {"Products": [{"Id": 1, "Name": "x"}]}}`))}
	_, err := s.Search(context.Background(), "x", 5)
	wantStoreError(t, err, "nemlig-søgning har ændret format.")

	h.f.respond[pSearch] = []any{jsonResp(200, J(t, `{"Products": {"Products": [{"Id": 1, "Name": "Gratis", "Price": 0}]}}`))}
	got, err := s.Search(context.Background(), "x", 5)
	if err != nil || len(got) != 1 || !got[0].Price.IsZero() {
		t.Errorf("an explicit price of 0 is a fact: %v %v", got, err)
	}
}

func offersPageProduct(t *testing.T, block, index int) obj {
	page := fixture(t, "offers_page").(obj)
	return page["pageContent"].([]any)[block].(obj)["products"].([]any)[index].(obj)
}

func TestOfferProductReading(t *testing.T) {
	// test_multi_buy_text_is_the_offer
	last := fixture(t, "offers_page").(obj)["pageContent"].([]any)
	lemon := last[len(last)-1].(obj)["products"].([]any)[1].(obj)
	if p, ok := offerProduct(lemon, "Priskup"); !ok || *p.OfferText != "2 stk. 15,-" {
		t.Errorf("%+v %v", p.OfferText, ok)
	}
	// test_offer_above_the_normal_price_is_not_an_offer
	dear := offersPageProduct(t, 1, 2)
	dear["price"] = json.Number("20000")
	if _, ok := offerProduct(dear, "Dagstilbud"); ok {
		t.Error("a price above the normal price is not an offer")
	}
	// isMulti is MULTI.match
	for text, want := range map[string]bool{
		"Mix 2 stk. 35,-": true, "2 stk. 15,-": true, "٢ stk. 15,-": true, "Mix  2 stk.": false,
		"mix 2 stk.": false, "2 stk": false, "Køb 2 stk. 15,-": false, "": false, "Mix stk.": false,
	} {
		if isMulti(text) != want {
			t.Errorf("isMulti(%q) = %v", text, !want)
		}
	}
}

// test_offers_limit_counts_only_real_offers: the page is changed, the gift search finds nothing.
func TestOffersLimitCountsOnlyRealOffers(t *testing.T) {
	h, s := envLogin(t)
	page := fixture(t, "offers_page").(obj)
	var block obj
	for _, b := range page["pageContent"].([]any) {
		if truthy(b.(obj)["products"]) {
			block = b.(obj)
			break
		}
	}
	products := block["products"].([]any)
	first := products[0].(obj)
	first["price"] = json.Number("99999") // not cheaper: filtered out
	page["pageContent"] = []any{block}
	h.f.respond[pPage] = []any{jsonResp(200, page)}
	h.f.respond[pSearch] = []any{jsonResp(200, J(t, `{"Products": {"Products": []}}`))}
	got, err := s.Offers(context.Background(), 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

// test_offers_without_lists_is_a_clear_error (Python emptied OFFER_LISTS)
func TestOffersWithoutKnownListsIsAClearError(t *testing.T) {
	_, s := envLogin(t)
	saved := offerLists
	offerLists = map[string]string{}
	defer func() { offerLists = saved }()
	_, err := s.Offers(context.Background(), 10)
	wantStoreError(t, err, "tilbudslister")
}

// recipe_slug and category_path, called directly in Python.
func TestSlugAndCategory(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.nemlig.com/maaltider/opskrift/kebabfad-98001303?x=1": "kebabfad-98001303",
		"/opskrifter/kebabfad-98001303":                                   "kebabfad-98001303",
		"  Kebabfad-98001303/#top ":                                       "kebabfad-98001303",
	} {
		if got, err := recipeSlug(in); err != nil || got != want {
			t.Errorf("recipeSlug(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"kebab%2f..", "kebab fad", "", "kebabfåd", "a\n/", "../x/..", "a_b"} {
		_, err := recipeSlug(bad)
		wantStoreError(t, err, "Kender ikke opskriften")
	}
	for in, want := range map[string]string{"Mad til flere dage": "mad-til-flere-dage", "Måltidskasse": "maaltidskasse", "ÆØÅ": "aeoeaa"} {
		if got, err := categoryPath(in); err != nil || got != want {
			t.Errorf("categoryPath(%q) = %q, %v", in, got, err)
		}
	}
	_, err := categoryPath("aftens/mad")
	wantStoreError(t, err, "Kender ikke kategorien")
}

// The helpers that read Python-style values.
func TestPythonStyleReaders(t *testing.T) {
	for text, want := range map[string]string{
		`5`: "5", `-0`: "0", `5.0`: "5.0", `1e22`: "1e+22", `1.5`: "1.5", `1e-05`: "1e-05", `123456789012345680000`: "123456789012345680000",
		`"x"`: "x", `null`: "None", `true`: "True", `[1, "a"]`: "[1, 'a']", `{"b": null, "a": [true]}`: "{'a': [True], 'b': None}",
	} {
		if got := pyStr(J(t, text)); got != want {
			t.Errorf("pyStr(%s) = %q, want %q", text, got, want)
		}
	}
	for text, want := range map[string]bool{`0`: false, `0.0`: false, `""`: false, `[]`: false, `{}`: false, `null`: false, `false`: false,
		`1`: true, `"0"`: true, `[0]`: true, `{"a": 0}`: true, `-0.5`: true, `true`: true} {
		if truthy(J(t, text)) != want {
			t.Errorf("truthy(%s) != %v", text, want)
		}
	}
	for text, want := range map[string]int{`4`: 4, `4.9`: 4, `-4.9`: -4, `"4"`: 4, `" 4 "`: 4, `"٤"`: 4, `"4_0"`: 40, `true`: 1} {
		if got := toInt(J(t, text)); got != want {
			t.Errorf("toInt(%s) = %d", text, got)
		}
	}
	for _, text := range []string{`"4.0"`, `"4-6"`, `null`, `[]`, `"abc"`, `1e400`} {
		func() {
			defer func() {
				if _, ok := recover().(shapeError); !ok {
					t.Errorf("toInt(%s) should be a shape error", text)
				}
			}()
			toInt(J(t, text))
		}()
	}
	if softInt(json.Number("0")) != nil || softInt("4-6") != nil || *softInt("4") != 4 {
		t.Error("softInt is _int(): 0 and junk are nil")
	}
	// Python's format() rounds half to even in _kr.
	for in, want := range map[string]string{"12.345": "12,34", "12.355": "12,36", "68": "68", "13.95": "13,95", "50.00": "50", "0.5": "0,50"} {
		m, _ := model.ParseMoney(in)
		if got := kr(m); got != want {
			t.Errorf("kr(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestQueueHost(t *testing.T) {
	for u, want := range map[string]bool{
		"https://nemlig.queue-it.net/?c=nemlig": true, "https://queue-it.net/": true, "https://A.Queue-It.NET:443/x": true,
		"https://www.nemlig.com/webapi/Token": false, "https://notqueue-it.net/": false, "https://queue-it.net.evil.example/": false,
	} {
		if isQueue(u) != want {
			t.Errorf("isQueue(%s) != %v", u, want)
		}
	}
}

// Trap 7: the headers are exactly the website's, a fresh correlation id each time, and the
// token header keeps its spelling.
func TestHeaders(t *testing.T) {
	isolate(t)
	t.Setenv("HANDLE_NEMLIG_APP_VERSION", "99.9.0")
	var seen []http.Header
	capture := roundTrip(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.Header.Clone())
		return build(req, jsonResp(200, obj{"access_token": "b", "Value": "x"})), nil
	})
	s := New(&http.Client{Transport: capture})
	if err := s.renewToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("%d requests", len(seen))
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	ids := map[string]bool{}
	for _, h := range seen {
		for k, want := range map[string]string{
			"Accept": "application/json, text/plain, */*", "Device-Size": "desktop", "Platform": "web",
			"Version": "99.9.0", "Referer": "https://www.nemlig.com/", "User-Agent": "Mozilla/5.0 (handle-mcp)",
		} {
			if got := h[k]; len(got) != 1 || got[0] != want {
				t.Errorf("%s = %v, want %q", k, got, want)
			}
		}
		id := h["X-Correlation-Id"]
		if len(id) != 1 || !uuid.MatchString(id[0]) || ids[id[0]] {
			t.Errorf("X-Correlation-Id %v is not a fresh UUID", id)
		}
		ids[id[0]] = true
	}
	if _, ok := seen[1]["Authorization"]; !ok || seen[1]["Authorization"][0] != "Bearer b" {
		t.Errorf("the second call carries the token: %v", seen[1]["Authorization"])
	}
	if _, ok := seen[0]["Authorization"]; ok {
		t.Error("the first call must not carry a token")
	}
	if _, _, err := s.send(context.Background(), call{method: "GET", url: base + "/x"}); err != nil {
		t.Fatal(err)
	}
	if got := seen[2]["X-XSRF-TOKEN"]; len(got) != 1 || got[0] != "x" { // not "X-Xsrf-Token"
		t.Errorf("the XSRF header must keep its spelling: %v", seen[2])
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A jar that forgets what it is given: a saved login must not be used then.
type forgetfulJar struct{}

func (forgetfulJar) SetCookies(*url.URL, []*http.Cookie) {}
func (forgetfulJar) Cookies(*url.URL) []*http.Cookie     { return nil }

func TestSavedSessionNeedsAReadableCookie(t *testing.T) {
	isolate(t)
	setSecret(t, `{"email":"kunde@example.com","session":{".ASPXAUTH":"auth-1"}}`)
	f := newFake(t)
	s := New(&http.Client{Transport: f, Jar: forgetfulJar{}})
	_, err := s.GetCart(context.Background())
	wantStoreError(t, err, "udløbet")
	if len(f.requests) != 0 {
		t.Errorf("nothing may be sent with a login that cannot be used: %v", urls(f.requests))
	}
}

// A cancelled call is the caller's cancel, not a store failure, and holds no URL.
func TestCancelledContext(t *testing.T) {
	_, s := envLogin(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, call := range map[string]func() error{
		"search": func() error { _, err := s.Search(ctx, "mælk", 5); return err },
		"cart":   func() error { _, err := s.GetCart(ctx); return err },
		"set": func() error {
			_, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: "1", Quantity: 1}})
			return err
		},
		"offers": func() error { _, err := s.Offers(ctx, 5); return err },
		"recipe": func() error { _, err := s.Recipe(ctx, "kebab-1", nil); return err },
	} {
		err := call()
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "http") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// No log line holds a password, an email, a token, a cookie or a query.
func TestLogsHoldNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	h, s := envLogin(t)
	ctx := context.Background()
	h.f.scripted[pCart] = []any{400}
	_, _ = s.GetCart(ctx)
	_, _ = s.Search(ctx, "hemmelig søgning", 5)
	_, _ = s.Order(ctx, "900001")
	_, _ = s.Offers(ctx, 5)
	h.f.respond[pSearch] = []any{jsonResp(200, J(t, `{"Products": {"Products": [{"Id": 1}]}}`))}
	_, _ = s.Search(ctx, "x", 5)
	t.Setenv("HANDLE_NEMLIG_PASSWORD", "forkert")
	_, s2 := newHarness(t)
	_, _ = s2.GetCart(ctx)

	log := buf.String()
	if log == "" {
		t.Fatal("expected some log lines")
	}
	for _, secret := range []string{"hemmeligt", "forkert", "kunde@example.com", "bearer-", "xsrf-1", "auth-1", "query=", "hemmelig+s", "900001?", "http"} {
		if strings.Contains(log, secret) {
			t.Errorf("the log holds %q:\n%s", secret, log)
		}
	}
}

// Trap 3: an answer in the wrong shape gives a clear StoreError (or a good answer), never a panic.
func TestWrongShapesNeverPanic(t *testing.T) {
	junk := []string{`[1, 2]`, `"text"`, `5`, `true`, `{"a": {}}`, `[{"x": 1}]`, `{"Products": {"Products": [1]}}`,
		`{"pageContent": [1]}`, `{"pageContent": [{"header": {"title": "Skarp pris"}, "products": "x"}]}`,
		`{"Lines": [1], "Orders": [1]}`, `{"Lines": "x"}`, `{"DayRangeHours": [1]}`, `{"DayRangeHours": [{"DayHours": [1]}]}`,
		`{"requestedCategory": {"recipes": [1]}, "searchResult": {"items": [1]}, "filters": [1]}`,
		`{"includedProducts": [1]}`, `{"includedProducts": [{"item": {"product": {"id": "1", "price": "x"}}}]}`,
		`{"Orders": null}`, `{"Orders": {"a": 1}}`, `{"ValidationFailures": [[1]], "Lines": [{"Quantity": "x", "Id": 1}]}`}
	paths := []string{pSearch, pPage, pDialog, pDetails, pAll, pRecipes, pCart, pAdd, pDays, pSpot, pHistory, pDetail}
	two := 2
	for _, text := range junk {
		for _, path := range paths {
			h, s := envLogin(t)
			for range 40 {
				h.f.respond[path] = append(h.f.respond[path], jsonResp(200, J(t, text)))
			}
			ctx := context.Background()
			calls := map[string]func() error{
				"search":   func() error { _, err := s.Search(ctx, "x", 5); return err },
				"offers":   func() error { _, err := s.Offers(ctx, 5); return err },
				"recipes":  func() error { _, err := s.Recipes(ctx, "x", 5); return err },
				"recipe":   func() error { _, err := s.Recipe(ctx, "kebabfad-98001303", &two); return err },
				"featured": func() error { _, err := s.FeaturedRecipes(ctx, 5); return err },
				"by_tag":   func() error { _, err := s.RecipesByTag(ctx, []string{"Familievenlig"}, nil, 5); return err },
				"cart":     func() error { _, err := s.GetCart(ctx); return err },
				"set": func() error {
					_, err := s.SetQuantities(ctx, []store.CartItem{{ProductID: "1", Quantity: 1}})
					return err
				},
				"terms":  func() error { _, err := s.Terms(ctx); return err },
				"info":   func() error { _, err := s.DeliveryInfo(ctx); return err },
				"slots":  func() error { _, err := s.DeliverySlots(ctx, 3); return err },
				"orders": func() error { _, err := s.Orders(ctx, 5); return err },
				"order":  func() error { _, err := s.Order(ctx, "900001"); return err },
			}
			for name, call := range calls {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("%s with %s at %s panicked: %v", name, text, path, r)
						}
					}()
					if err := call(); err != nil {
						var se *store.StoreError
						if !errors.As(err, &se) {
							t.Errorf("%s with %s at %s: %T is not a StoreError", name, text, path, err)
						}
					}
				}()
			}
		}
	}
}

// SetQuantities sends the items in the order asked, absolute, once each.
func TestSetQuantitiesKeepsTheOrderAsked(t *testing.T) {
	h, s := envLogin(t)
	items := []store.CartItem{{ProductID: "9", Quantity: 3}, {ProductID: "2", Quantity: 0}, {ProductID: "5", Quantity: 1}}
	results, err := s.SetQuantities(context.Background(), items)
	if err != nil || len(results) != 3 {
		t.Fatalf("%v %v", results, err)
	}
	var sent []string
	h.f.mu.Lock()
	for _, r := range h.f.requests {
		if strings.HasSuffix(r.URL, "/AddToBasket") {
			b := r.Body.(obj)
			sent = append(sent, b["ProductId"].(string)+"="+string(b["quantity"].(json.Number)))
			if b["AffectPartialQuantity"] != false || b["disableQuantityValidation"] != false {
				t.Errorf("body %v", b)
			}
		}
	}
	h.f.mu.Unlock()
	if strings.Join(sent, ",") != "9=3,2=0,5=1" {
		t.Errorf("sent %v", sent)
	}
	for i, r := range results {
		if r.ProductID != items[i].ProductID || !r.OK {
			t.Errorf("result %d: %+v", i, r)
		}
	}
}

// The settings cache runs on the injected clock, to the second.
func TestSettingsTTLBoundary(t *testing.T) {
	for wait, wantCalls := range map[time.Duration]int{599 * time.Second: 1, 600 * time.Second: 2, 601 * time.Second: 2} {
		h, s := envLogin(t)
		ctx := context.Background()
		_, _ = s.Search(ctx, "a", 5)
		h.clock = h.clock.Add(wait)
		_, _ = s.Search(ctx, "b", 5)
		if got := h.f.count("/webapi/v2/AppSettings/Website"); got != wantCalls {
			t.Errorf("after %v: %d settings calls, want %d", wait, got, wantCalls)
		}
	}
}

// A login nemlig refuses is one attempt per Search, not two (ADR 0005): repeated attempts can lock
// the account. Python swallowed the refusal in the settings step and Search tried again.
func TestWrongPasswordIsOneLoginPerSearch(t *testing.T) {
	isolate(t)
	t.Setenv("HANDLE_NEMLIG_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_NEMLIG_PASSWORD", "forkert")
	h, s := newHarness(t)
	for call := 1; call <= 2; call++ {
		_, err := s.Search(context.Background(), "mælk", 10)
		wantStoreError(t, err, "nemlig.com afviser login. Tjek email og kodeord.")
		if got := h.f.count("/webapi/login"); got != call {
			t.Fatalf("after search %d: %d POSTs to /webapi/login, want %d", call, got, call)
		}
	}
	if h.f.count("/searchgateway/api/search") != 0 {
		t.Error("search must not run after a refused login")
	}
}

func TestDeadSavedSessionIsOneLoginPerSearch(t *testing.T) {
	isolate(t)
	setSecret(t, `{"email": "kunde@example.com", "session": {".ASPXAUTH": "dead"}}`)
	h, s := newHarness(t)
	for call := 1; call <= 2; call++ {
		_, err := s.Search(context.Background(), "mælk", 10)
		wantStoreError(t, err, "Login til nemlig er udløbet")
		// The attempt with a saved session is the check of the basket for an email.
		if got := h.f.count("/webapi/basket/GetBasket"); got != call {
			t.Fatalf("after search %d: %d session checks, want %d", call, got, call)
		}
	}
	if h.f.count("/webapi/login") != 0 || h.f.count("/webapi/v2/AppSettings/Website") != 0 {
		t.Errorf("unexpected calls: %v", h.f.paths())
	}
}

// Offers and recipes share the settings step, so they follow the same rule.
func TestWrongPasswordIsOneLoginForOffers(t *testing.T) {
	isolate(t)
	t.Setenv("HANDLE_NEMLIG_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_NEMLIG_PASSWORD", "forkert")
	h, s := newHarness(t)
	_, err := s.Offers(context.Background(), 5)
	wantStoreError(t, err, "nemlig.com afviser login. Tjek email og kodeord.")
	if got := h.f.count("/webapi/login"); got != 1 {
		t.Fatalf("%d POSTs to /webapi/login, want 1", got)
	}
}
