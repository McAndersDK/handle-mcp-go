package bilkatogo

// Named exceptions for pickup (docs/adr/0006). The goldens are the recorded Python answers and are
// not edited. For every case the answer is compared after goldentest.DropAdded removed the two new
// keys, so the golden proves everything else. A case whose answer really changes because of pickup
// is listed here with a hand-written expectation, and is compared WITH the new keys: it never
// goes through DropAdded. If a case outside this list changes, its golden test fails.

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// wantPickupCases is the number of named pickup exceptions.
const wantPickupCases = 10

const (
	// The texts Python recorded, which the goldens still hold.
	oldNotHome   = "Kurven er sat til afhentning, ikke hjemlevering. Vælg hjemlevering på bilkatogo.dk."
	oldNoAddress = "Ingen leveringsadresse valgt i BilkaToGo. Vælg hjemlevering på bilkatogo.dk."
)

// pickupException is one case. Exactly one of set, full or err is the expectation:
//   - set: the golden answer with these top-level keys replaced (values are JSON text);
//   - full: a whole hand-written answer, for a case that was an error in the golden;
//   - err: the new error text (the golden had was).
type pickupException struct {
	set  map[string]string
	full string
	err  string
	was  string // the text the golden has, so an exception cannot drift from what it replaces
}

const store9002 = `"Bilka Eksempelby"`

var pickupExceptions = map[string]pickupException{
	// A pickup cart (cart_logged_in.json) is pickup now: no "choose home delivery" warning.
	"cartpickup": {set: map[string]string{
		"warnings": `[]`, "delivery_mode": `"pickup"`, "pickup_store": store9002}},
	// The delivery_price of 0 is a known fee for pickup, not a missing one.
	"termspickup": {set: map[string]string{
		"delivery_fee": `"0.00"`, "delivery_mode": `"pickup"`, "pickup_store": store9002}},
	"delivery_infopickup": {set: map[string]string{
		"note": `null`, "delivery_mode": `"pickup"`, "pickup_store": store9002}},
	// The pickup cart with the fake's default answer for AvailableDeliveryDates: the same two
	// intervals as slots_default, now pickup times with the fee 0.00.
	"slotspickup": {full: `[
	  {"available": false, "end": "2026-11-02T06:00:00+01:00", "kind": "Afhentning",
	   "note": "Levering vil ske natten til mandag. Vi kommer med dine varer imellem 20:00 og 06:00.  For at benytte det valgte tidsrum, skal du bestille senest på søndag kl. 15:00, bemærk at tidspunktet kan blive fuldt booket forinden.",
	   "pickup_store": "Bilka Eksempelby", "price": "0.00", "start": "2026-11-02T00:00:00+01:00", "store": "bilkatogo"},
	  {"available": true, "end": "2026-11-04T06:00:00+01:00", "kind": "Afhentning",
	   "note": "Levering vil ske natten til onsdag. Vi kommer med dine varer imellem 20:00 og 06:00.  For at benytte det valgte tidsrum, skal du bestille senest i morgen kl. 15:00, bemærk at tidspunktet kan blive fuldt booket forinden.",
	   "pickup_store": "Bilka Eksempelby", "price": "0.00", "start": "2026-11-04T00:00:00+01:00", "store": "bilkatogo"}]`},
	// No address, or no way to tell: the unknown mode (fail closed).
	"cart_no_address": {set: map[string]string{
		"warnings":      `["` + notKnown + `", "373,00 kr. fra minimumskøb på 500,00 kr. alt inkl. for at få hjemmelevering"]`,
		"delivery_mode": `null`, "pickup_store": `null`}},
	"cart_minimal": {set: map[string]string{
		"warnings": `["` + notKnown + `"]`, "delivery_mode": `null`, "pickup_store": `null`}},
	"delivery_info_no_address": {set: map[string]string{
		"note": `"` + notKnown + `"`, "delivery_mode": `null`, "pickup_store": `null`}},
	"search_no_address":          {err: noAddress, was: oldNoAddress},
	"search_address_without_sid": {err: noAddress, was: oldNoAddress},
	"slots_not_home":             {err: notKnown, was: oldNotHome},
}

// pickupAnswer is the hand-written expectation for a case, or ok=false when the case is not listed.
func pickupAnswer(t *testing.T, name string, golden json.RawMessage) (want any, e pickupException, ok bool) {
	t.Helper()
	e, ok = pickupExceptions[name]
	if !ok {
		return nil, e, false
	}
	switch {
	case e.full != "":
		want = J(t, e.full)
	case e.set != nil:
		g, derr := decodeJSON(golden)
		m, isMap := g.(map[string]any)
		if derr != nil || !isMap {
			t.Fatalf("%s: the golden answer is not an object", name)
		}
		for k, text := range e.set {
			if k == "delivery_mode" || k == "pickup_store" {
				if _, there := m[k]; there {
					t.Fatalf("%s: the golden already has %s", name, k)
				}
			}
			m[k] = J(t, text)
		}
		want = m
	}
	return want, e, true
}

// checkPickupException compares one step of a listed case. It returns true when it handled it.
func checkPickupException(t *testing.T, caseName string, step goldenStep, answer any, err error, ex *exceptions) bool {
	t.Helper()
	want, e, ok := pickupAnswer(t, caseName, step.Answer)
	if !ok {
		return false
	}
	ex.pickup[caseName] = true
	if e.err != "" {
		if step.Error == nil || step.Error.Message != e.was {
			t.Errorf("%s: the golden should hold the error %q", caseName, e.was)
		}
		var se *store.StoreError
		if !errors.As(err, &se) || se.Msg != e.err {
			t.Errorf("%s: error %v, want %q", caseName, err, e.err)
		}
		return true
	}
	if err != nil {
		t.Errorf("%s: unexpected error: %v", caseName, err)
		return true
	}
	if e.full != "" && step.Error == nil {
		t.Errorf("%s: a full answer replaces a golden error, but the golden has an answer", caseName)
	}
	if !equalAnswer(want, toPlain(t, answer), caseName, ex) { // with the new keys: no DropAdded here
		t.Errorf("%s: answer differs:\n got  %s\n want %s", caseName, jsonText(toPlain(t, answer)), jsonText(want))
	}
	return true
}
