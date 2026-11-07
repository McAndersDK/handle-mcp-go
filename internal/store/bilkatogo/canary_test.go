package bilkatogo

// ADR 0005, privacy test 2: personal data an adapter could leak by mistake. A transport deep-merges
// the LEAK map of upstream/tests/conftest.py into every JSON object of every answer (merging, not
// overwriting, so real values survive and the cart's deliveryAddress keeps the fields that tell
// home from pickup). The read methods run through the real adapter, the results go through
// privacy.Out, and no LEAK value may be in the output.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

// leak is LEAK in upstream/tests/conftest.py.
func leak() obj {
	return obj{
		"CustomerName":    "Anders And",
		"Email":           "anders@example.com",
		"MobileNumber":    "+45 98 76 54 32",
		"DoorCode":        "kode-4711",
		"UnattendedNotes": "Nøglen ligger under måtten",
		"deliveryAddress": obj{"street": "Andeby 1", "zip": "postnr-9999", "city": "Andeby"},
	}
}

func leakValues() []string {
	return []string{
		"Anders And", "anders@example.com", "+45 98 76 54 32", "kode-4711",
		"Nøglen ligger under måtten", "Andeby 1", "postnr-9999", "Andeby",
	}
}

// fixtureValues are the personal values in the fixtures themselves (a home cart holds the
// customer's name and street in deliveryAddress; orders hold a name and a pickup address).
func fixtureValues() []string {
	return []string{"Kunde Eksempel", "Testvej", "Eksempelby", "Test Kunde", "kunde@example.com", "900001", "eyFake", "0a3f50a0", "87654321"}
}

// mergeLeak puts the leak into every object of v. A key that is already there stays (also null);
// two objects are merged one level down.
func mergeLeak(v any) any {
	switch x := v.(type) {
	case obj:
		for k, e := range x {
			x[k] = mergeLeak(e)
		}
		mergeInto(x, leak())
		return x
	case []any:
		for i, e := range x {
			x[i] = mergeLeak(e)
		}
	}
	return v
}

func mergeInto(dst, src obj) {
	for k, sv := range src {
		dv, there := dst[k]
		switch {
		case !there:
			dst[k] = sv
		case isObj(dv) && isObj(sv):
			mergeInto(dv.(obj), sv.(obj))
		}
	}
}

func isObj(v any) bool { _, ok := v.(obj); return ok }

type leaky struct{ inner http.RoundTripper }

func (l leaky) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.inner.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if v, err := decodeJSON(raw); err == nil && len(raw) > 0 {
		if raw, err = json.Marshal(mergeLeak(v)); err != nil {
			return nil, err
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	return resp, nil
}

func TestCanaryMergeKeepsRealValues(t *testing.T) {
	v := mergeLeak(J(t, `[{"Email": "kunde@example.com", "Nested": {"x": 1}, "deliveryAddress": {"dawa_uid": "x"}, "DoorCode": null}]`))
	o := v.([]any)[0].(obj)
	if o["Email"] != "kunde@example.com" || o["DoorCode"] != nil {
		t.Errorf("a real value was overwritten: %v", o)
	}
	addr := o["deliveryAddress"].(obj)
	if addr["dawa_uid"] != "x" || addr["city"] != "Andeby" {
		t.Errorf("addresses were not merged: %v", addr)
	}
	if o["Nested"].(obj)["CustomerName"] != "Anders And" || o["CustomerName"] != "Anders And" {
		t.Errorf("the leak is not in every object: %v", o)
	}
}

func readAll(t *testing.T, h *harness) map[string]func() (any, error) {
	return map[string]func() (any, error){
		"search": func() (any, error) { addressWith(`{"sid": "9002"}`)(h); return h.s.Search(bg, "skyr", 5) },
		"offers": func() (any, error) { addressWith(`{"sid": "9001"}`)(h); return h.s.Offers(bg, 10) }, // each store id has its own fixture products
		"cart":   func() (any, error) { return h.s.GetCart(bg) },
		"terms":  func() (any, error) { return h.s.Terms(bg) },
		"info":   func() (any, error) { return h.s.DeliveryInfo(bg) },
		"slots":  func() (any, error) { return h.s.DeliverySlots(bg, 3) },
		"orders": func() (any, error) { return h.s.Orders(bg, 5) },
		"order":  func() (any, error) { return h.s.Order(bg, "900000000002") },
	}
}

func TestCanaryNothingLeaksThroughTheReadMethods(t *testing.T) {
	h := envLogin(t)
	h.s = New(&http.Client{Transport: leaky{h.f}}, WithClock(func() time.Time { return h.clock }))
	// The canary is really in the answers the adapter reads.
	probe, err := h.s.cartJSON(bg)
	if err != nil || !strings.Contains(jsonText(probe), "Anders And") {
		t.Fatalf("the canary is not in the answers (%v)", err)
	}
	for name, read := range readAll(t, h) {
		t.Run(name, func(t *testing.T) {
			result, err := read()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := privacy.Out(result)
			if err != nil || len(out) < 20 {
				t.Fatalf("%s: no answer (%v)", name, err)
			}
			for _, secret := range append(leakValues(), fixtureValues()...) {
				if strings.Contains(out, secret) {
					t.Errorf("%s leaks %q:\n%s", name, secret, out)
				}
			}
		})
	}
}

// A home cart holds the customer's name in deliveryAddress.name. It never gets out, and the
// address is read for the few facts we need only.
func TestHomeAddressNameNeverLeaves(t *testing.T) {
	h := envLogin(t)
	for name, read := range readAll(t, h) {
		result, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, _ := privacy.Out(result)
		for _, secret := range fixtureValues() {
			if strings.Contains(out, secret) {
				t.Errorf("%s leaks %q", name, secret)
			}
		}
	}
	got := readAddress(h.f.cart)
	if !got.home || got.sid == "" {
		t.Errorf("address facts: %+v", got)
	}
}
