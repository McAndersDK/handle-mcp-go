package bilkatogo

// Tests written by hand for the traps of the port, and the Go home of each test in
// tests/test_bilkatogo.py that the goldens do not already cover. The mapping:
//
//	test_login_flow_and_jwt_header               golden cart_home_password (requests, headers)
//	test_saved_session_logs_in_without_password  golden cart_session
//	test_dead_session_says_log_in_again          golden cart_dead_session
//	test_gigya_busy_is_not_an_expired_session    golden cart_gigya_rate_limit, cart_gigya_server_error
//	test_an_old_saved_password_is_replaced...    golden cart_home_password (keychain_after)
//	test_env_vars_are_never_saved                TestEnvVarsAreNeverSaved
//	test_login_never_sends_cart_cookie           TestLoginNeverSendsCartCookie (4 ways)
//	test_wrong_password_is_a_clear_error         golden cart_wrong_password
//	test_expired_session_body_logs_in_again      golden cart_expired_body_logs_in_again
//	test_other_error_body_is_not_a_logout        golden cart_other_error_body
//	test_retries_on_503                          golden cart_503_twice
//	test_retries_on_network_error                golden cart_network_errors
//	test_search_uses_cart_store_and_parses       golden search_skyr
//	test_search_without_address_says_what...     golden search_no_address
//	test_home_delivery_cart                      golden cart_home_password
//	test_pickup_cart_warns                       golden cartpickup, termspickup
//	test_set_quantities_ok_refused_and_limited   golden set_ok_refused_limited
//	test_terms_and_delivery_info                 golden terms_default, delivery_info_default
//	test_orders_and_order                        golden orders_default, order_default
//	test_missing_order_is_clear                  golden order_missing
//	test_never_calls_forbidden_endpoints         TestNeverCallsForbiddenEndpoints
//	test_tools_hide_personal_data_from_bilka     canary_test.go (TestCanary..., TestHomeAddressNameNeverLeaves)
//	test_old_chosen_slot_is_ignored              golden cart_slot_ended, delivery_info_slot_ended
//	test_current_chosen_slot_shows_in_the_cart   golden cart_home_password
//	test_unreadable_slot_keeps_the_cart_text     golden cart_slot_unreadable
//	test_set_quantities_stops_on_store_failure   golden set_store_failure_stops
//	test_money_in_ore_is_robust                  TestKr
//	test_offer_has_before_price_and_offer...     TestOfferHasBeforePrice
//	test_se_her_gets_a_real_text                 TestSeHerGetsARealText
//	test_no_offer_has_no_before_price            TestNoOfferHasNoBeforePrice
//	test_offers_filter_on_our_store              golden offers_sid_9001
//	test_delivery_slots_flat_fee_and_night...    golden slots_7
//	test_delivery_slots_keep_exactly_days        golden slots_1, slots_3
//	test_delivery_slots_say_when_the_fee...      golden slots_fee_unknown
//	test_delivery_slots_need_home_delivery       golden slots_not_home
//	test_offers_without_a_login_say_why          golden offers_no_login

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

var bg = context.Background()

// envLogin is a world with the password in env vars: every call logs in with Gigya.
func envLogin(t *testing.T) *harness {
	t.Helper()
	isolate(t)
	t.Setenv("HANDLE_BILKATOGO_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_BILKATOGO_PASSWORD", "hemmeligt")
	return newHarness(t)
}

func storeErr(t *testing.T, err error) string {
	t.Helper()
	var se *store.StoreError
	if !errors.As(err, &se) {
		t.Fatalf("want a StoreError, got %#v", err)
	}
	return se.Msg
}

func TestKr(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`12345`, "123.45"}, {`"1250"`, "12.50"}, {`null`, "0.00"}, {`0`, "0.00"}, {`""`, "0.00"},
		{`1250.5`, "12.51"}, {`"1250.5"`, "12.51"}, {`3900`, "39.00"},
	} {
		if got := kr(J(t, c.in)).String(); got != c.want {
			t.Errorf("kr(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	for in, repr := range map[string]string{`"abc"`: "'abc'", `true`: "True", `[1]`: "[1]"} {
		var err error
		func() {
			defer guard(&err)
			kr(J(t, in))
		}()
		if got := storeErr(t, err); got != "BilkaToGo sendte et ukendt beløb: "+repr+"." {
			t.Errorf("kr(%s) error = %q", in, got)
		}
	}
}

func TestLoginNeverSendsCartCookie(t *testing.T) {
	loginsOK := func(t *testing.T, h *harness, want int) {
		t.Helper()
		logins := h.f.all(pLoginJW)
		if len(logins) != want {
			t.Fatalf("%d LoginJWT calls, want %d", len(logins), want)
		}
		for i, l := range logins {
			if slices.Contains(l.Cookies, "cart") {
				t.Errorf("LoginJWT call %d carried the cart cookie: %v", i+1, l.Cookies)
			}
		}
	}
	t.Run("relogin after the JWT ran out", func(t *testing.T) {
		h := envLogin(t)
		if _, err := h.s.GetCart(bg); err != nil {
			t.Fatal(err)
		}
		h.clock = h.clock.Add(25 * time.Hour)
		if _, err := h.s.GetCart(bg); err != nil {
			t.Fatal(err)
		}
		loginsOK(t, h, 2)
		carts := h.f.all(pCart)
		if !slices.Contains(carts[1].Cookies, "cart") {
			t.Errorf("the cookie the new login set should be used afterwards: %v", carts[1].Cookies)
		}
	})
	t.Run("relogin after an expired body", func(t *testing.T) {
		h := envLogin(t)
		if _, err := h.s.GetCart(bg); err != nil {
			t.Fatal(err)
		}
		scriptedJSON(pCart, expired)(h)
		if _, err := h.s.GetCart(bg); err != nil {
			t.Fatal(err)
		}
		loginsOK(t, h, 2)
	})
	t.Run("a cookie with its own domain and path", func(t *testing.T) {
		h := envLogin(t)
		h.f.setCookie = "cart=abc; Domain=bilkatogo.dk; Path=/api"
		for range 3 {
			if _, err := h.s.GetCart(bg); err != nil {
				t.Fatal(err)
			}
			expireJWT(h)
		}
		loginsOK(t, h, 3)
		if c := h.f.all(pCart)[2].Cookies; !slices.Contains(c, "cart") {
			t.Errorf("cart cookie lost: %v", c)
		}
	})
	t.Run("a cookie the jar was never told about", func(t *testing.T) {
		// Set behind the jar's back, as a call still in flight could set it again at any moment.
		h := envLogin(t)
		h.s.jar.inner.SetCookies(mustURL(apiURL), []*http.Cookie{{Name: "cart", Value: "zzz", Path: "/"}})
		if _, err := h.s.GetCart(bg); err != nil {
			t.Fatal(err)
		}
		loginsOK(t, h, 1)
		if !slices.Contains(h.f.all(pCart)[0].Cookies, "cart") {
			t.Error("the guard may only strip the cookie from the login call")
		}
	})
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	h := envLogin(t)
	h.f.respond[pCart] = []any{&resp{status: 302, headers: map[string]string{"Location": "https://elsewhere.example/x"}}}
	_, err := h.s.GetCart(bg)
	if got := storeErr(t, err); !strings.Contains(got, "svarede uventet") {
		t.Errorf("error = %q", got)
	}
	for _, r := range h.f.requests {
		if strings.Contains(r.URL, "elsewhere") {
			t.Errorf("a redirect was followed: %s", r.URL)
		}
	}
	h = envLogin(t)
	h.f.scripted[pLoginJW] = []any{302}
	if _, err := h.s.GetCart(bg); storeErr(t, err) != "BilkaToGo login fejlede (302)." {
		t.Errorf("LoginJWT redirect: %v", err)
	}
}

func TestJWTLifetime(t *testing.T) {
	h := envLogin(t)
	if _, err := h.s.GetCart(bg); err != nil {
		t.Fatal(err)
	}
	h.clock = h.clock.Add(86099 * time.Second) // 24 h minus 5 min, less a second
	if _, err := h.s.GetCart(bg); err != nil || h.f.count(pLoginJW) != 1 {
		t.Fatalf("logged in again too early (%d logins, %v)", h.f.count(pLoginJW), err)
	}
	h.clock = h.clock.Add(time.Second)
	if _, err := h.s.GetCart(bg); err != nil || h.f.count(pLoginJW) != 2 {
		t.Fatalf("did not log in again at 24 h minus 5 min (%d logins, %v)", h.f.count(pLoginJW), err)
	}
}

func TestEnvVarsAreNeverSaved(t *testing.T) {
	h := envLogin(t)
	if _, err := h.s.GetCart(bg); err != nil {
		t.Fatal(err)
	}
	if got := h.s.Session(); got["login_token"] != "st2.fake-login-token" {
		t.Errorf("session = %v", got)
	}
	if _, err := keyring.Get(store.KeychainService, name); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("a login from env vars was saved: %v", err)
	}
}

func TestSessionBeforeLoginIsNil(t *testing.T) {
	if got := New(nil).Session(); got != nil {
		t.Errorf("session = %v", got)
	}
}

func TestNeverCallsForbiddenEndpoints(t *testing.T) {
	h := envLogin(t)
	calls := []func() error{
		func() error { _, err := h.s.Search(bg, "mælk", 10); return err },
		func() error { _, err := h.s.Offers(bg, 10); return err },
		func() error {
			_, err := h.s.SetQuantities(bg, []store.CartItem{{ProductID: "18381", Quantity: 2}})
			return err
		},
		func() error { _, err := h.s.Terms(bg); return err },
		func() error { _, err := h.s.DeliveryInfo(bg); return err },
		func() error { _, err := h.s.DeliverySlots(bg, 7); return err },
		func() error { _, err := h.s.Orders(bg, 10); return err },
		func() error { _, err := h.s.Order(bg, "900000000002"); return err },
	}
	for i, c := range calls {
		if err := c(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	for _, p := range h.f.paths() {
		for _, bad := range []string{"EmptyCart", "SetDelivery", "Checkout", "Payment", "PlaceOrder", "ChangeAddress"} {
			if strings.Contains(strings.ToLower(p), strings.ToLower(bad)) {
				t.Errorf("forbidden endpoint called: %s", p)
			}
		}
	}
	// ChangeLineCount is the one cart write, and only SetQuantities makes it.
	if n := h.f.count(pChange); n != 1 {
		t.Errorf("%d ChangeLineCount calls, want 1", n)
	}
}

// --- products: shapes seen live 2026-11-01 (docs/research.md), amounts in øre ---------------------

func hit(offer string, price, now, show int) obj {
	data := obj{
		"price": json.Number(itoa(now)), "inStock": true, "offerDescription": offer,
		"unitsOfMeasurePrice": json.Number("6500"), "unitsOfMeasureShowPrice": json.Number(itoa(show)),
		"unitsOfMeasurePriceUnit": "Kg.",
	}
	return obj{"objectID": "1", "name": "Hakket kyllingekød", "price": json.Number(itoa(price)), "storeData": obj{"s": data}}
}

func itoa(n int) string { return strconv.Itoa(n) }

func parse(t *testing.T, h obj) *model.Product {
	t.Helper()
	var p *model.Product
	var err error
	func() {
		defer guard(&err)
		p = product(h, "s", "søgning")
	}()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOfferHasBeforePrice(t *testing.T) {
	p := parse(t, hit("Skarp pris", 6500, 3900, 3900))
	if p.Price.String() != "39.00" || p.UnitPrice.String() != "39.00" || p.BeforePrice.String() != "65.00" {
		t.Errorf("%v %v %v", p.Price, p.UnitPrice, p.BeforePrice)
	}
	if p.OfferText == nil || *p.OfferText != "Skarp pris" {
		t.Errorf("offer text %v", p.OfferText)
	}
}

func TestSeHerGetsARealText(t *testing.T) {
	p := parse(t, hit("Se Her", 7495, 6900, 8625))
	if p.OfferText == nil || *p.OfferText != "Tilbud, før 74,95 kr" || !p.OnOffer {
		t.Errorf("offer text %v", p.OfferText)
	}
	// Python formats half to even in this one text: 74.945 is 74.94, 74.955 is 74.96.
	for in, want := range map[string]string{"7494.5": "Tilbud, før 74,94 kr", "7495.5": "Tilbud, før 74,96 kr"} {
		h := hit("Se Her", 0, 6900, 8625)
		h["price"] = J(t, in)
		if got := *parse(t, h).OfferText; got != want {
			t.Errorf("before %s: %q, want %q", in, got, want)
		}
	}
	if got := *parse(t, hit("Se Her", 6900, 6900, 8625)).OfferText; got != "Tilbud" {
		t.Errorf("no before price: %q", got)
	}
}

func TestNoOfferHasNoBeforePrice(t *testing.T) {
	p := parse(t, hit("", 10995, 8500, 14167))
	if p.OnOffer || p.BeforePrice != nil || p.UnitPrice.String() != "141.67" {
		t.Errorf("%+v", p)
	}
}

// ADR 0005: a missing price is a format change, never a free product. An explicit 0 is 0.00 kr.
func TestMissingPriceIsNotAFreeProduct(t *testing.T) {
	for _, offers := range []bool{false, true} {
		h := envLogin(t)
		h.f.respond[pAlgolia] = []any{jsonResp(200, obj{"results": []any{obj{"hits": []any{
			obj{"objectID": "1", "name": "Uden pris", "storeData": obj{"9002": obj{"inStock": true}}},
		}}}})}
		var err error
		if offers {
			_, err = h.s.Offers(bg, 5)
		} else {
			_, err = h.s.Search(bg, "x", 5)
		}
		want := "BilkaToGo-søgning har ændret format."
		if offers {
			want = "BilkaToGo-tilbud har ændret format."
		}
		if got := storeErr(t, err); got != want {
			t.Errorf("offers=%v: %q, want %q", offers, got, want)
		}
	}
	null := hit("", 1, 1, 1)
	null["storeData"].(obj)["s"].(obj)["price"] = nil
	var err error
	func() { defer guard(&err); product(null, "s", "søgning") }()
	if err == nil {
		t.Error("a null price must be refused too")
	}
	zero := hit("", 0, 0, 0)
	if p := parse(t, zero); p.Price.String() != "0.00" {
		t.Errorf("an explicit 0 is 0.00 kr, got %s", p.Price)
	}
}

// A unit without any unit price is unknown, never 0.00 kr per unit (it would sort first).
func TestMissingUnitPriceIsUnknown(t *testing.T) {
	h := hit("", 1000, 1000, 0)
	data := h["storeData"].(obj)["s"].(obj)
	delete(data, "unitsOfMeasureShowPrice")
	delete(data, "unitsOfMeasurePrice")
	p := parse(t, h)
	if p.UnitPrice != nil || p.Unit == nil || *p.Unit != "kg" {
		t.Errorf("unit price %v, unit %v", p.UnitPrice, p.Unit)
	}
	data["unitsOfMeasurePrice"] = json.Number("0")
	if p := parse(t, h); p.UnitPrice == nil || p.UnitPrice.String() != "0.00" {
		t.Errorf("an explicit 0 stays 0.00: %v", p.UnitPrice)
	}
}

func TestPyJSONString(t *testing.T) {
	if got, want := pyJSONString("a\"b\\é\n\x7f\U0001F600"), "\"a\\\"b\\\\\\u00e9\\n\\u007f\\ud83d\\ude00\""; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDeliveryModes(t *testing.T) {
	for _, c := range []struct {
		addr string
		want mode
	}{
		{`null`, modeUnknown}, {`{}`, modeUnknown},
		{`{"sid": "1234"}`, modeUnknown},                               // a store id alone says nothing
		{`{"sid": "1234", "name": "Bilka Eksempelby"}`, modePickup},    // the pickup rule
		{`{"sid": "1234", "name": "Kunde Eksempel"}`, modeUnknown},     // not a store name
		{`{"name": "Bilka Eksempelby"}`, modeUnknown},                  // no store id
		{`{"sid": "1", "name": "Bilka X", "dawa_uid": "x"}`, modeHome}, // a home marker wins
		{`{"dawa_uid": "x"}`, modeHome}, {`{"recipient_name": "y", "sid": "1"}`, modeHome},
	} {
		if got := deliveryMode(readAddress(obj{"deliveryAddress": J(t, c.addr)})); got != c.want {
			t.Errorf("%s: mode %d, want %d", c.addr, got, c.want)
		}
	}
}

func TestSlotsDaysAreCalendarDaysFromTheClock(t *testing.T) {
	h := envLogin(t)
	for days, want := range map[int]int{1: 1, 3: 2, 14: 2} { // fixture: Nov 2 and Nov 4 (Nov 3 sold out)
		slots, err := h.s.DeliverySlots(bg, days)
		if err != nil || len(slots) != want {
			t.Errorf("days=%d: %d slots (%v), want %d", days, len(slots), err, want)
		}
	}
	h.clock = h.clock.Add(24 * time.Hour) // today is Nov 3: the first day is out of the range
	slots, err := h.s.DeliverySlots(bg, 1)
	if err != nil || len(slots) != 1 { // Nov 2 is not after Nov 3; Nov 4 is
		t.Errorf("%d slots (%v)", len(slots), err)
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
