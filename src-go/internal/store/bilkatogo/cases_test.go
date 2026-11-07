package bilkatogo

// What each golden case did to the fake backend before its steps ran. The golden files record the
// calls and the answers, not the setup, so this table is the Go copy of the setups in
// scripts/golden_bilka.py and golden_bilka_b.py (read as text). Entry 0 of a case is its setup;
// entry n is the hook the Python case ran before its n-th extra step.

import (
	"encoding/json"
	"testing"
	"time"
)

// harness is what a case hook can change.
type harness struct {
	tb    testing.TB
	f     *fake
	s     *Store
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

const (
	pCart    = "/api/shop/v6/Cart"
	pChange  = "/api/shop/v6/ChangeLineCount"
	pAvail   = "/api/shop/v3/AvailableDeliveryDates"
	pAlgolia = "/1/indexes/*/queries"
	pJWT     = "/accounts.getJWT"
	pLogin   = "/accounts.login"
	pLoginJW = "/api/auth/LoginJWT"
	pHistory = "/api/shop/v7/OrderHistory"
	pDetails = "/api/shop/v7/OrderDetails"
)

const expired = `{"code": 1, "msg": "Du er ikke logget ind"}`

// --- hooks ------------------------------------------------------------------

func scripted(path string, items ...any) hook {
	return func(h *harness) { h.f.scripted[path] = append([]any(nil), items...) }
}

func scriptedJSON(path string, texts ...string) hook {
	return func(h *harness) {
		var items []any
		for _, t := range texts {
			items = append(items, J(h.tb, t))
		}
		h.f.scripted[path] = items
	}
}

func respond(path string, rs ...any) hook {
	return func(h *harness) { h.f.respond[path] = append([]any(nil), rs...) }
}

func bodyAt(path, text string) hook {
	return func(h *harness) { h.f.respond[path] = []any{jsonResp(200, J(h.tb, text))} }
}

func bodyValue(path string, v any) hook {
	return func(h *harness) { h.f.respond[path] = []any{jsonResp(200, v)} }
}

// cartWith changes the cart: the keys of the JSON text are set, removed keys are deleted.
func cartWith(text string, removed ...string) hook {
	return func(h *harness) {
		if text != "" {
			for k, v := range J(h.tb, text).(obj) {
				h.f.cart[k] = v
			}
		}
		for _, k := range removed {
			delete(h.f.cart, k)
		}
	}
}

// slotWith is cart(deliveryDate={**SLOT, **changes}).
func slotWith(changes string) hook {
	return func(h *harness) {
		slot := fixture(h.tb, "cart_home").(obj)["deliveryDate"].(obj)
		for k, v := range J(h.tb, changes).(obj) {
			slot[k] = v
		}
		h.f.cart["deliveryDate"] = slot
	}
}

// addressWith is cart(deliveryAddress={**HOME["deliveryAddress"], **changes}).
func addressWith(changes string) hook {
	return func(h *harness) {
		addr := fixture(h.tb, "cart_home").(obj)["deliveryAddress"].(obj)
		for k, v := range J(h.tb, changes).(obj) {
			addr[k] = v
		}
		h.f.cart["deliveryAddress"] = addr
	}
}

func pickup(h *harness) { h.f.cart = fixture(h.tb, "cart_logged_in").(obj) }

func expireJWT(h *harness) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	h.s.jwt = ""
}

func C(hooks ...hook) []hook { return hooks }

// bilkaDays is an AvailableDeliveryDates answer with one day and the given (start, end) intervals.
func bilkaDays(date string, intervals [][2]string) obj {
	var ivs []any
	for _, iv := range intervals {
		ivs = append(ivs, obj{
			"intervalStart": iv[0], "intervalEnd": iv[1], "availability_bit": json.Number("1"), "closed": json.Number("0"),
			"info": []any{obj{"message": iv[0] + "-" + iv[1]}},
		})
	}
	return obj{"availableIntervals": []any{obj{"date": date, "intervals": ivs}}}
}

var (
	dstSpring = [][2]string{{"01:30", "02:30"}, {"02:00", "03:00"}, {"02:30", "02:45"}, {"03:00", "06:00"}, {"23:00", "01:00"}}
	dstAutumn = [][2]string{{"00:00", "06:00"}, {"01:00", "02:30"}, {"02:00", "03:00"}, {"02:30", "02:45"}, {"23:00", "01:00"}}
)

func daysAt(date string, ivs [][2]string) hook {
	return func(h *harness) { h.f.respond[pAvail] = []any{jsonResp(200, bilkaDays(date, ivs))} }
}

const closedAndBits = `{"availableIntervals": [
 {"date": "2026-11-03", "intervals": [
  {"intervalStart": "08:00", "intervalEnd": "10:00", "availability_bit": 0, "closed": 0},
  {"intervalStart": "10:00", "intervalEnd": "12:00", "availability_bit": 3, "closed": 1},
  {"intervalStart": "12:00", "intervalEnd": "14:00", "availability_bit": 2, "closed": 0},
  {"intervalStart": "14:00", "intervalEnd": "16:00", "closed": 0},
  {"intervalStart": "16:00", "intervalEnd": "18:00", "availability_bit": 5, "closed": 0,
   "info": [{"message": "<b>Nat</b> a"}, {}]},
  {"intervalStart": "18:00", "intervalEnd": "18:00", "availability_bit": 1},
  {"intervalStart": "x", "intervalEnd": "20:00", "availability_bit": 1},
  {"intervalStart": "20:00", "intervalEnd": "21:00", "availability_bit": 1}]},
 {"date": "garbage", "intervals": [{"intervalStart": "08:00", "intervalEnd": "10:00"}]},
 {"date": "2026-11-04"},
 {"date": "2026-11-05", "intervals": null}]}`

const oddOrders = `{"orders": [
 {"orderId": 5, "status": "Faktureret", "packedPrice": 1250, "ordered": {"date": "2026-10-01"}},
 {"status": "Mystisk", "orderedPrice": "300", "deliveryDate": "2026-10-02", "deliveryInterval": {"start": "08:00:00"}},
 {"status": null, "price": 100, "ordered": {"date": "2026-10-03", "time": "08:15:30"}},
 {}]}`

func repeatErr(n int, err error) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = err
	}
	return out
}

// table is the setup of every golden case, by name. A case with no entry fails the test.
func table() map[string][]hook {
	scriptErrs := func(path string, errs ...any) hook {
		return func(h *harness) { h.f.scripted[path] = errs }
	}
	two := func(path, a, b string) hook { return scriptedJSON(path, a, b) }
	nothing := C()
	return map[string][]hook{
		// get_cart
		"cart_home_password":               nothing,
		"cart_session":                     nothing,
		"cart_dead_session":                nothing,
		"cart_wrong_password":              nothing,
		"cart_no_login":                    nothing,
		"cart_invalid_keychain":            nothing,
		"cart_session_without_token":       nothing,
		"cart_gigya_rate_limit":            C(scriptedJSON(pJWT, `{"errorCode": 403048, "errorMessage": "busy"}`)),
		"cart_gigya_server_error":          C(scriptedJSON(pJWT, `{"errorCode": 500001, "errorMessage": "busy"}`)),
		"cart_gigya_other_error":           C(scriptedJSON(pLogin, `{"errorCode": 400006, "errorMessage": "Nej"}`)),
		"cart_gigya_error_without_message": C(scriptedJSON(pLogin, `{"errorCode": 7}`)),
		"cart_gigya_error_code_text":       C(scriptedJSON(pLogin, `{"errorCode": "abc", "errorMessage": "x"}`)),
		"cart_gigya_not_json":              C(respond(pLogin, textResp(200, "<html>"))),
		"cart_gigya_503_gives_up":          C(scripted(pLogin, 503, 503, 503)),
		"cart_login_without_token":         C(scriptedJSON(pLogin, `{"errorCode": 0, "sessionInfo": {}}`)),
		"cart_login_without_jwt":           C(scriptedJSON(pJWT, `{"errorCode": 0}`)),
		"cart_loginjwt_403":                C(scripted(pLoginJW, 403)),
		"cart_loginjwt_503_gives_up":       C(scripted(pLoginJW, 503, 503, 503)),
		"cartpickup":                       C(pickup),
		"cart_slot_ended":                  C(slotWith(`{"deliveryDate": "2024-09-04"}`)),
		"cart_slot_ends_today_before_now":  C(slotWith(`{"deliveryDate": "2026-11-02", "intervalStart": "09:00", "intervalEnd": "11:59"}`)),
		"cart_slot_ends_today_after_now":   C(slotWith(`{"deliveryDate": "2026-11-02", "intervalStart": "11:00", "intervalEnd": "12:01"}`)),
		"cart_slot_unreadable":             C(slotWith(`{"intervalEnd": null}`)),
		"cart_no_slot":                     C(cartWith("", "deliveryDate")),
		"cart_no_address":                  C(cartWith(`{"deliveryAddress": null}`)),
		"cart_messages":                    C(cartWith(`{"messages": [{"text": "a<b>b</b>"}, "plain", {"nope": 1}]}`)),
		"cart_minimal":                     C(bodyAt(pCart, `{}`)),
		"cart_fees_zero":                   C(cartWith(`{"stat": {"price": 5000, "minimum_left": 0, "delivery_price": 0, "deposit": 250}}`)),
		"cart_amount_not_a_number":         C(cartWith(`{"stat": {"price": "abc"}}`)),
		"cart_amount_decimal_text":         C(cartWith(`{"stat": {"price": "1250.5", "delivery_price": 3900}}`)),
		"cart_expired_body_logs_in_again":  C(nil, scriptedJSON(pCart, expired)),
		"cart_expired_twice":               C(nil, two(pCart, expired, expired)),
		"cart_other_error_body":            C(nil, scriptedJSON(pCart, `{"code": null, "msg": "Noget gik galt", "eid": 500}`)),
		"cart_401_logs_in_again":           C(nil, scripted(pCart, 401)),
		"cart_401_twice":                   C(nil, scripted(pCart, 401, 401)),
		"cart_503_twice":                   C(scripted(pCart, 503, 503)),
		"cart_503_gives_up":                C(scripted(pCart, 503, 503, 503)),
		"cart_network_errors":              C(scriptErrs(pCart, errReadError, errRemoteProtocol)),
		"cart_timeout_gives_up":            C(scriptErrs(pCart, repeatErr(3, errConnectTimeout)...)),
		"cart_other_http_error":            C(scriptErrs(pCart, errUnsupportedType)),
		"cart_400":                         C(scripted(pCart, 400)),
		"cart_not_json":                    C(respond(pCart, textResp(200, "<html>"))),
		"cart_json_list":                   C(bodyValue(pCart, []any{json.Number("1")})),
		"cart_jwt_expires_logs_in_again":   C(nil, expireJWT),
		"cart_logs_in_once":                C(nil, nil),
		"session_before_login":             nothing,
		// search and offers
		"search_skyr":                   nothing,
		"search_default_limit":          nothing,
		"search_session":                nothing,
		"search_sid_1653":               C(addressWith(`{"sid": "1653"}`)),
		"search_no_login":               nothing,
		"search_no_address":             C(cartWith(`{"deliveryAddress": null}`)),
		"search_address_without_sid":    C(cartWith(`{"deliveryAddress": {"sid": null}}`)),
		"search_fior_di_latte":          nothing,
		"search_mozzarella":             nothing,
		"search_hvidloeg":               nothing,
		"search_makrel_tomat":           nothing,
		"search_hakkede_tomater":        nothing,
		"search_403":                    C(scripted(pAlgolia, 403)),
		"search_503_gives_up":           C(scripted(pAlgolia, 503, 503, 503)),
		"search_format_changed":         C(bodyAt(pAlgolia, `{"results": []}`)),
		"search_not_json":               C(respond(pAlgolia, textResp(200, "<html>"))),
		"search_hit_without_store_data": C(bodyAt(pAlgolia, `{"results": [{"hits": [{"objectID": "1", "name": "A"}]}]}`)),
		"offers_default":                nothing,
		"offers_default_limit":          nothing,
		"offers_sid_1653":               C(addressWith(`{"sid": "1653"}`)),
		"offers_no_login":               nothing,
		// set_quantities
		"set_ok_refused_limited":               nothing,
		"set_refused_by_message_body":          C(scriptedJSON(pChange, `{"code": null, "msg": "Nej tak"}`)),
		"set_store_failure_stops":              C(scripted(pChange, 503, 503, 503)),
		"set_applied_is_text":                  C(scriptedJSON(pChange, `{"productAmount": "3", "message": ""}`)),
		"set_applied_missing":                  C(scriptedJSON(pChange, `{"message": ""}`)),
		"set_both_messages":                    C(scriptedJSON(pChange, `{"productAmount": 2, "message": "a", "offerLimitMessage": "b"}`)),
		"set_only_offer_limit_message":         C(scriptedJSON(pChange, `{"productAmount": 2, "message": "", "offerLimitMessage": "kun 2"}`)),
		"set_expired_body_logs_in_again":       C(scriptedJSON(pChange, expired)),
		"set_remove":                           nothing,
		"set_empty":                            nothing,
		"set_sends_absolute_count_per_product": nothing,
		// terms, delivery info, slots
		"terms_default":                 nothing,
		"termspickup":                   C(pickup),
		"terms_no_address":              C(cartWith(`{"deliveryAddress": null, "stat": {}}`)),
		"terms_session":                 nothing,
		"delivery_info_default":         nothing,
		"delivery_info_slot_ended":      C(slotWith(`{"deliveryDate": "2024-09-04"}`)),
		"delivery_infopickup":           C(pickup),
		"delivery_info_no_address":      C(cartWith(`{"deliveryAddress": null}`)),
		"delivery_info_no_slot_no_next": C(cartWith(`{"deliveryAddress": {"dawa_uid": "x"}}`, "deliveryDate")),
		"slots_7":                       nothing,
		"slots_default":                 nothing,
		"slots_1":                       nothing,
		"slots_3":                       nothing,
		"slots_14":                      nothing,
		"slots_fee_unknown":             C(cartWith(`{"stat": {}}`)),
		"slots_not_home":                C(cartWith(`{"deliveryAddress": {"sid": "1234"}}`)),
		"slotspickup":                   C(pickup),
		"slots_session":                 nothing,
		"slots_dst_spring_2026_03_29":   C(daysAt("2026-03-29", dstSpring)),
		"slots_dst_autumn_2026_10_25":   C(daysAt("2026-10-25", dstAutumn)),
		"slots_dst_day_before_spring":   C(daysAt("2026-03-28", dstSpring)),
		"slots_dst_day_after_autumn":    C(daysAt("2026-10-26", dstAutumn)),
		"slots_closed_and_bits":         C(bodyAt(pAvail, closedAndBits)),
		"slots_no_dates":                C(bodyAt(pAvail, `{}`)),
		"slots_503_gives_up":            C(scripted(pAvail, 503, 503, 503)),
		// orders
		"orders_default":            nothing,
		"orders_default_limit":      nothing,
		"orders_empty":              C(bodyAt(pHistory, `{}`)),
		"orders_odd_entries":        C(bodyAt(pHistory, oddOrders)),
		"order_default":             nothing,
		"order_session":             nothing,
		"order_missing":             C(scriptedJSON(pDetails, `{"order": null}`)),
		"order_no_order_key":        C(scriptedJSON(pDetails, `{}`)),
		"order_id_is_sent_as_given": nothing,
		"order_message_body":        C(scriptedJSON(pDetails, `{"code": null, "msg": "Ingen adgang"}`)),
		// recipes: BilkaToGo has none
		"recipes":          nothing,
		"recipe":           nothing,
		"featured_recipes": nothing,
		"recipes_by_tag":   nothing,
	}
}
