package nemlig

// What each golden case did to the fake backend before its steps ran. The golden files record the
// calls and the answers, not the setup, so this table is the Go copy of the setups in
// scripts/golden_nemlig_a.py, _b.py, _c.py and _r.py (read as text). Step 0 is the setup; step n
// is the hook the Python case ran before its n-th extra step.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"testing"
	"time"
)

// harness is what a case hook can change.
type harness struct {
	tb    testing.TB
	f     *fake
	jar   *testJar
	clock time.Time
}

type hook func(h *harness)

// J reads JSON text into plain values.
func J(tb testing.TB, text string) any {
	tb.Helper()
	v, err := decodeJSON([]byte(text))
	if err != nil {
		tb.Fatalf("bad JSON in the case table: %v", err)
	}
	return v
}

// testJar is a cookie jar that can be emptied: httpx's cookies.clear().
type testJar struct {
	mu  sync.Mutex
	jar *cookiejar.Jar
}

func newTestJar() *testJar { j := &testJar{}; j.Clear(); return j }
func (j *testJar) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar, _ = cookiejar.New(nil)
}
func (j *testJar) SetCookies(u *url.URL, c []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar.SetCookies(u, c)
}
func (j *testJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jar.Cookies(u)
}

const (
	pSearch  = "/searchgateway/api/search"
	pPage    = "/productbff/api/web/page"
	pDialog  = "/productbff/api/web/meals/recipeProductDialog"
	pDetails = "/productbff/api/web/meals/recipeDetails/kebabfad-med-fladbrod-98001303"
	pAll     = "/productbff/api/web/meals/allRecipes"
	pRecipes = "/productbff/api/web/search/recipes"
	pLogin   = "/webapi/login"
	pCart    = "/webapi/basket/GetBasket"
	pAdd     = "/webapi/basket/AddToBasket"
	pDays    = "/webapi/v2/Delivery/GetDeliveryDays"
	pSpot    = "/webapi/Order/DeliverySpot"
	pHistory = "/webapi/order/GetBasicOrderHistory"
	pDetail  = "/webapi/v2/order/GetOrderHistory/900001"
)

// --- hooks ------------------------------------------------------------------

func scripted(path string, items ...any) hook {
	return func(h *harness) { h.f.scripted[path] = append([]any(nil), items...) }
}

func respond(path string, rs ...any) hook {
	return func(h *harness) { h.f.respond[path] = append([]any(nil), rs...) }
}

func bodyAt(path, text string) hook {
	return func(h *harness) { h.f.respond[path] = []any{jsonResp(200, J(h.tb, text))} }
}

func put(change func(f *fake)) hook { return func(h *harness) { change(h.f) } }

func queueOn(f *fake)    { f.queue = true }
func apiQueueOn(f *fake) { f.apiQueue = true }

// basketWith changes the basket: keys of the JSON text are set, removed keys are deleted.
func basketWith(text string, removed ...string) hook {
	return func(h *harness) {
		if text != "" {
			for k, v := range J(h.tb, text).(obj) {
				h.f.basket[k] = v
			}
		}
		for _, k := range removed {
			delete(h.f.basket, k)
		}
	}
}

func advance(seconds int) hook {
	return func(h *harness) { h.clock = h.clock.Add(time.Duration(seconds) * time.Second) }
}

func forgetCookies(h *harness) { h.jar.Clear() }

func all(hooks ...hook) hook {
	return func(h *harness) {
		for _, k := range hooks {
			k(h)
		}
	}
}

// the human logs in between two steps: a login appears in the keychain
func saveLogin(h *harness) { setSecret(h.tb, `{"email":"kunde@example.com","password":"hemmeligt"}`) }

func C(hooks ...hook) []hook { return hooks }

var queueIt = redirect("https://nemlig.queue-it.net/?c=nemlig")

func readErrors(n int, err error) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = err
	}
	return out
}

const offerPage = `{"pageContent": [
 {"header": {"title": "Skarp pris"}, "products": [
  {"id": 1, "title": "Dyrere", "price": 20000, "priceOriginal": 15895},
  {"id": 2, "title": "Gave", "price": 0, "priceOriginal": 2045, "labelMaxQuantity": 1},
  {"id": 3, "title": "Uden før-pris", "price": 1000, "availability": {"type": "SoldOut"},
   "pricePerUnit": {"value": 5000, "unitType": "Weight"}, "description": "200 g"},
  {"id": 4, "title": "Mix", "price": 1500, "priceOriginal": 2000, "labelMaxQuantity": 3,
   "campaignLines": [{"text": "Mix 2 stk. 35,-"}, {"text": "x"}],
   "pricePerUnit": {"value": 7550, "unitType": "Pieces"}},
  {"id": 5, "title": "Lige pris", "price": 1500, "priceOriginal": 1500},
  {"id": 6, "title": "Volumen", "price": 999, "priceOriginal": 1999,
   "pricePerUnit": {"value": 1, "unitType": "Volume"}}]},
 {"header": {"title": "  DAGSTILBUD "}, "products": null},
 {"header": {"title": "Frugt og grønt"}, "products": [{"id": 9, "title": "x", "price": 1}]}]}`

const row = `{"ingredientTitle": "Løg", "item": {"product": {"id": "1", "title": "Løg"}}}`

// rowZero is the same row with an explicit price of 0: a real fact that Python and Go both accept.
const rowZero = `{"ingredientTitle": "Løg", "item": {"product": {"id": "1", "title": "Løg", "price": 0}}}`

func brokenSkarpPris(h *harness) {
	page := fixture(h.tb, "offers_page").(obj)
	skarp := page["pageContent"].([]any)[1].(obj)
	delete(skarp["products"].([]any)[0].(obj), "id")
	h.f.respond[pPage] = []any{jsonResp(200, page)}
}

func recipeOdd(r string) hook {
	return func(h *harness) {
		h.f.respond[pDialog] = []any{jsonResp(200, J(h.tb, `{"includedProducts": [`+r+`], "servings": "4-6"}`))}
		h.f.respond[pDetails] = []any{jsonResp(200, J(h.tb, `{"servings": "4-6",
	  "images": [{"source": "http://x/plain.jpg"}, {"source": null}],
	  "instructions": [{"title": null, "steps": [{"description": "  "}]}]}`))}
	}
}

func recipeTexts(r string) hook {
	return func(h *harness) {
		h.f.respond[pDialog] = []any{jsonResp(200, J(h.tb, `{"includedProducts": [`+r+`], "servings": 6}`))}
		h.f.respond[pDetails] = []any{jsonResp(200, J(h.tb, `{"servings": 4,
	  "ingredients": [{"ingredients": [{"text": "600 g laksefilet"}, {"text": ""}]}],
	  "images": [{"source": "http://insecure.example/x.jpg"}, {"source": " https://ok.example/a.jpg "}],
	  "instructions": [{"title": "Tom", "steps": [{"description": "  "}, {}]},
	                   {"title": null, "steps": [{"description": " Rør rundt "}]}],
	  "tip": "  Server med brød  ", "description": " Intro ", "durationTotal": {"from": 25},
	  "servingLimits": {"minServings": 2, "maxServings": 8}}`))}
	}
}

func recipeNoPrice(r string) hook {
	return func(h *harness) {
		h.f.respond[pDialog] = []any{jsonResp(200, J(h.tb, `{"includedProducts": [`+r+`, {"item": {"product": {}}}, {}]}`))}
		h.f.respond[pDetails] = []any{jsonResp(200, J(h.tb, `{}`))}
	}
}

func days(date string, hours [][2]int, deadline string) hook {
	return func(h *harness) {
		if deadline == "" {
			deadline = date + "T00:00:00"
		}
		var hs []any
		for i, hr := range hours {
			hs = append(hs, obj{
				"Id": 9000 + i, "StartHour": hr[0], "EndHour": hr[1],
				"DeliveryPrice": jsonNum(fmt.Sprintf("%d.0", 19+i)), "Deadline": deadline,
				"Date": date + "T00:00:00", "Type": i % 2, "Availability": 0,
			})
		}
		page := obj{"DayRangeHours": []any{obj{"DayHours": hs, "Date": date + "T00:00:00"}}, "NextRangeStart": nil}
		h.f.respond[pDays] = []any{jsonResp(200, page)}
	}
}

var dstHours = [][2]int{{0, 2}, {1, 3}, {2, 3}, {2, 4}, {3, 5}, {4, 6}, {7, 9}, {14, 21}, {22, 24}, {23, 2}}

const oddSlots = `{"DayRangeHours": [{"DayHours": [
 {"StartHour": null, "EndHour": 9, "Date": "2026-11-03T00:00:00"},
 {"StartHour": 7, "EndHour": 9, "Date": "0001-01-01T00:00:00"},
 {"StartHour": 7, "EndHour": 9, "Date": "tekst"},
 {"StartHour": 7, "EndHour": 9, "Date": "2026-11-03T00:00:00", "Availability": 1, "Type": 5, "DeliveryPrice": null},
 {"StartHour": 9, "EndHour": 11, "Date": "2026-11-03T00:00:00", "Availability": 2, "Type": 1,
  "DeliveryPrice": 0, "Deadline": "2026-11-03T08:05:00"},
 {"StartHour": 11, "EndHour": 13, "Date": "2026-11-03T00:00:00", "Availability": 0, "Type": 0,
  "DeliveryPrice": 12.5, "Deadline": "garbage"}]}]}`

const unknownStatus = `{"Orders": [
 {"Id": 1, "Status": 9, "Total": 10, "OrderDate": "2026-11-03T14:21:00",
  "DeliveryTime": {"Start": "2026-11-03T14:00:00", "End": "2026-11-03T21:00:00"}},
 {"Status": null}, {"Id": 2, "Status": 2, "Total": null}]}`

const orderWithTotal = `{"Id": "900001", "Total": 99.5, "Status": 4,
 "Lines": [{"ProductNumber": 1, "ProductName": "A", "Quantity": 2, "Amount": 10}]}`

// The hooks that need the *testing.T are built at run time, so the table holds funcs.
func table() map[string][]hook {
	body := bodyAt
	noSlot := basketWith("", "DeliveryTimeSlot")
	return map[string][]hook{
		// --- search
		"search_cocio": C(), "search_default_limit": C(), "search_gift": C(), "search_kyllingebryst_10": C(),
		"search_kyllingebryst_50": C(), "search_broccoli": C(), "search_hvidloeg": C(),
		"search_hakkede_tomater": C(), "search_hakkede_tomater_30": C(), "search_makrel_tomat": C(),
		"search_session_login": C(), "search_no_login": C(), "search_invalid_keychain": C(),
		"search_wrong_password":         C(),
		"search_login_without_redirect": C(body(pLogin, `{}`)),
		"search_login_not_json":         C(respond(pLogin, textResp(200, "<html>"))),
		"search_login_503":              C(scripted(pLogin, 503, 503, 503)),
		"search_pages_in_queue":         C(put(queueOn)),
		"search_settings_fail":          C(scripted(pCart, 404)),
		"search_settings_reused":        C(),
		"search_settings_expire":        C(nil, advance(601)),
		"search_settings_not_expired":   C(nil, advance(599)),
		"search_format_changed":         C(body(pSearch, `{"Products": null}`)),
		"search_format_changed_key":     C(body(pSearch, `{"Products": {}}`)),
		"search_empty_body":             C(respond(pSearch, emptyResp(200))),
		"search_not_json":               C(respond(pSearch, textResp(200, "<html>nej</html>"))),
		"search_401_renewed":            C(scripted(pSearch, 401)),
		"search_401_login_again":        C(scripted(pSearch, 401, 401)),
		"search_401_refused":            C(scripted(pSearch, 401, 401, 401)),
		"search_401_refused_session":    C(scripted(pSearch, 401, 401, 401)),
		"search_503_twice":              C(scripted(pSearch, 503, 503)),
		"search_503_gives_up":           C(scripted(pSearch, 503, 503, 503)),
		"search_429_gives_up":           C(scripted(pSearch, 429, 429, 429)),
		"search_400":                    C(scripted(pSearch, 400)),
		"search_404":                    C(scripted(pSearch, 404)),
		"search_read_error_then_ok":     C(scripted(pSearch, errReadError, 503)),
		"search_timeout_gives_up":       C(scripted(pSearch, readErrors(3, errReadTimeout)...)),
		"search_other_http_error":       C(scripted(pSearch, errUnsupported)),
		"search_queue_it_redirect":      C(respond(pSearch, queueIt)),
		// --- offers
		"offers_default": C(), "offers_default_limit": C(), "offers_limit_1": C(), "offers_session": C(),
		"offers_no_login":                    C(),
		"offers_anonymous_token_renewed":     C(nil, scripted(pPage, 401)),
		"offers_anonymous_refused":           C(nil, scripted(pPage, 401, 401)),
		"offers_login_saved_after_anonymous": C(nil, saveLogin),
		"offers_no_timeslot":                 C(noSlot),
		"offers_one_broken_list":             C(brokenSkarpPris),
		"offers_all_broken": C(body(pPage, `{"pageContent": [
		  {"header": {"title": "Skarp pris"}, "products": [{}]},
		  {"header": {"title": "Dagstilbud"}, "products": [{"id": 1}]}]}`)),
		"offers_no_lists":                   C(body(pPage, `{"pageContent": []}`)),
		"offers_empty_body":                 C(respond(pPage, emptyResp(200))),
		"offers_parsing_edge_cases":         C(body(pPage, offerPage)),
		"offers_queue_it_api":               C(put(apiQueueOn)),
		"offers_pages_in_queue":             C(put(queueOn)),
		"offers_gift_search_fails":          C(scripted(pSearch, 500, 500, 500)),
		"offers_gift_search_format_changed": C(body(pSearch, `{"Products": null}`)),
		// --- recipes
		"recipes_search": C(), "recipes_search_default_limit": C(), "recipes_search_no_timeslot": C(noSlot),
		"recipes_search_format_changed":  C(body(pRecipes, `{"searchResult": {}}`)),
		"recipes_search_item_without_id": C(body(pRecipes, `{"searchResult": {"items": [{"title": "x"}, {"id": "a-1"}]}}`)),
		"recipe_kebab_url":               C(), "recipe_kebab_old_path": C(), "recipe_kebab_no_persons": C(),
		"recipe_kebab_persons_0": C(), "recipe_slug_query_fragment": C(), "recipe_session": C(),
		"recipe_bad_slug_percent": C(), "recipe_bad_slug_space": C(), "recipe_bad_slug_empty": C(),
		"recipe_bad_slug_unicode":          C(),
		"recipe_format_changed":            C(body(pDialog, `{}`)),
		"recipe_dialog_empty_body":         C(respond(pDialog, emptyResp(200))),
		"recipe_dialog_500":                C(scripted(pDialog, 500, 500, 500)),
		"recipe_details_404":               C(scripted(pDetails, 404)),
		"recipe_odd_servings_and_images":   C(recipeOdd(row)),
		"recipe_texts_and_limits":          C(recipeTexts(row)),
		"recipe_without_price_or_servings": C(recipeNoPrice(row)),
		"featured_default":                 C(), "featured_default_limit": C(), "featured_3": C(),
		"featured_queue_it_api": C(put(apiQueueOn)),
		"featured_no_blocks":    C(body(pPage, `{"pageContent": []}`)),
		"featured_items_without_id": C(body(pPage, `{"pageContent": [
		  {"header": {"title": "T"}, "recipes": [{"title": "x"}, {"id": "a-1", "title": "A"}]},
		  {"recipes": null}, {"recipes": [{"id": "b-2"}]}]}`)),
		"by_tag_family_budget": C(), "by_tag_default_limit": C(), "by_tag_under_30_madpakke": C(),
		"by_tag_category_with_spaces": C(), "by_tag_category_danish_letters": C(), "by_tag_bad_category": C(),
		"by_tag_unknown": C(), "by_tag_no_tags": C(), "by_tag_cached": C(),
		"by_tag_no_filters": C(body(pAll, `{"filters": []}`)),
		"by_tag_search_format_changed": C(func(h *harness) {
			h.f.respond[pAll] = []any{jsonResp(200, fixture(h.tb, "recipes_by_tag")), jsonResp(200, obj{})}
		}),
		// --- cart
		"cart_default": C(), "cart_session": C(), "cart_password_replaced_by_session": C(),
		"cart_dead_session": C(), "cart_wrong_password": C(), "cart_no_login": C(), "cart_invalid_keychain": C(),
		"cart_hides_removed_lines": C(basketWith(`{"Lines": [
		  {"Id": "5064654", "Name": "Vare", "Quantity": 1, "Price": 10.0},
		  {"Id": "9", "Name": "Æg", "Quantity": 0, "Price": 28.0},
		  {"Id": 7, "Quantity": null, "Price": null}]}`)),
		"cart_empty":                                C(basketWith(`{"Lines": [], "TotalPrice": 0, "TotalProductsPrice": 0, "NumberOfProducts": 0}`)),
		"cart_minimal":                              C(body(pCart, `{"Email": "kunde@example.com"}`)),
		"cart_total_missing":                        C(basketWith("", "TotalPrice")),
		"cart_products_total_missing":               C(basketWith("", "TotalProductsPrice")),
		"cart_fees_zero_or_missing":                 C(basketWith(`{"DeliveryPrice": 0, "TotalBagsPrice": null, "TotalDepositsPrice": 3.5}`)),
		"cart_validation_failures":                  C(basketWith(`{"ValidationFailures": [{"Message": "Mangler"}, "tekst", {"Andet": 1}, 5]}`)),
		"cart_minimum_met":                          C(basketWith(`{"IsMinTotalValid": true, "MinimumOrderTotal": 0}`)),
		"cart_anonymous_basket_is_ok_with_password": C(basketWith(`{"Email": null}`)),
		"cart_401_renewed":                          C(nil, scripted(pCart, 401)),
		"cart_session_refused_everywhere":           C(nil, scripted(pCart, 401, 401, 401)),
		"cart_session_cookie_dropped":               C(nil, forgetCookies),
		"cart_503_twice":                            C(scripted(pCart, 503, 503)),
		"cart_503_gives_up":                         C(scripted(pCart, 503, 503, 503)),
		"cart_read_error":                           C(scripted(pCart, errReadError, 503)),
		"cart_400":                                  C(scripted(pCart, 400)),
		"cart_not_json":                             C(respond(pCart, textResp(200, "<html>"))),
		"cart_queue_it":                             C(respond(pCart, redirect("https://nemlig.queue-it.net/"))),
		"cart_logs_in_once":                         C(),
		// --- set quantities
		"set_absolute_and_remove": C(), "set_sold_out": C(), "set_session": C(), "set_dead_session": C(),
		"set_one_failure_continues":  C(scripted(pAdd, 400)),
		"set_all_fail_503":           C(scripted(pAdd, 503, 503, 503)),
		"set_empty":                  C(),
		"set_session_cookie_dropped": C(nil, forgetCookies),
		"set_response_not_json":      C(respond(pAdd, textResp(200, "x"))),
		"set_empty_answer":           C(respond(pAdd, emptyResp(200))),
		"set_two_then_cart":          C(),
		// --- terms
		"terms_default": C(), "terms_session": C(),
		"terms_empty_cart_estimates_bags": C(basketWith(`{"Lines": [], "TotalBagsPrice": 0}`)),
		"terms_empty_cart_bag_fee_comma":  C(basketWith(`{"Lines": []}`)),
		"terms_empty_cart_bag_fee_dot":    C(basketWith(`{"Lines": []}`)),
		"terms_empty_cart_bag_fee_bad":    C(basketWith(`{"Lines": []}`)),
		"terms_empty_cart_bag_fee_empty":  C(basketWith(`{"Lines": []}`)),
		"terms_products_no_bags":          C(basketWith(`{"TotalBagsPrice": 0}`)),
		"terms_removed_lines_only":        C(basketWith(`{"Lines": [{"Id": "9", "Name": "Æg", "Quantity": 0, "Price": 28.0}], "TotalBagsPrice": 0}`)),
		"terms_minimal":                   C(body(pCart, `{"Email": "x"}`)),
		// --- delivery
		"delivery_info_default":        C(),
		"delivery_info_no_chosen_time": C(basketWith(`{"FormattedDeliveryTime": null}`)),
		"delivery_info_spot_empty":     C(respond(pSpot, emptyResp(200))),
		"delivery_info_odd_times": C(body(pSpot, `{"TimeSlot": {"Start": "0001-01-01T00:00:00", "End": "garbage"},
		  "EditDeadline": "9999-12-31T23:59:59"}`)),
		"delivery_info_naive_and_utc_times": C(body(pSpot, `{"TimeSlot": {"Start": "2026-11-03T16:00:00Z",
		  "End": "2026-11-03T19:00:00+01:00"}, "EditDeadline": "2026-11-02T23:00:00"}`)),
		"slots_7": C(), "slots_default": C(), "slots_3": C(), "slots_1": C(), "slots_14": C(),
		"slots_endless_pages": C(put(func(f *fake) { f.endlessDays = true })),
		"slots_second_page_fails": C(func(h *harness) {
			h.f.respond[pDays] = []any{jsonResp(200, fixture(h.tb, "delivery_days")), emptyResp(503), emptyResp(503), emptyResp(503)}
		}),
		"slots_dst_spring_2026_03_29": C(days("2026-03-29", dstHours, "2026-03-28T12:00:00")),
		"slots_dst_autumn_2026_10_25": C(days("2026-10-25", dstHours, "2026-10-24T12:00:00")),
		"slots_dst_day_before_spring": C(days("2026-03-28", dstHours, "")),
		"slots_dst_day_after_autumn":  C(days("2026-10-26", dstHours, "")),
		"slots_summer_and_winter":     C(days("2026-07-01", [][2]int{{0, 3}, {14, 21}}, "")),
		"slots_odd_entries":           C(body(pDays, oddSlots)),
		"slots_empty":                 C(body(pDays, `{}`)),
		"slots_empty_body":            C(respond(pDays, emptyResp(200))),
		"slots_503_gives_up":          C(scripted(pDays, 503, 503, 503)),
		"slots_session":               C(),
		// --- orders
		"orders_default": C(), "orders_default_limit": C(),
		"orders_flex":           C(put(func(f *fake) { f.history = "order_history_flex" })),
		"orders_empty":          C(body(pHistory, `{}`)),
		"orders_null_body":      C(respond(pHistory, emptyResp(200))),
		"orders_unknown_status": C(body(pHistory, unknownStatus)),
		"order_900001":          C(),
		"order_flex":            C(put(func(f *fake) { f.details = "order_details_flex" })),
		"order_not_found":       C(body(pDetail, `{}`)),
		"order_404":             C(scripted(pDetail, 404)),
		"order_with_total":      C(body(pDetail, orderWithTotal)),
		"order_id_empty":        C(), "order_id_path": C(), "order_id_query": C(), "order_id_arabic_digits": C(),
		"order_id_with_newline": C(), "order_id_21_digits": C(), "order_id_20_digits": C(), "order_session": C(),
	}
}

func jsonNum(s string) json.Number { return json.Number(s) }

// zeroPriceVariants are the three recipe cases again with a price of 0 written in the row. They
// must give exactly the recorded answer: only a MISSING price changed (see missingPriceCases).
var zeroPriceVariants = map[string][]hook{
	"recipe_odd_servings_and_images":   C(recipeOdd(rowZero)),
	"recipe_texts_and_limits":          C(recipeTexts(rowZero)),
	"recipe_without_price_or_servings": C(recipeNoPrice(rowZero)),
}
