package nemlig

// ADR 0005, privacy test 2: personal data an adapter could leak by mistake. A transport deep-merges
// the LEAK map of upstream/tests/conftest.py into every JSON object of every answer (merging, not
// overwriting, so real values survive). The read methods run through the real adapter, the results
// go through privacy.Out, and no LEAK value may be in the output.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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
	v := mergeLeak(J(t, `[{"Email": "kunde@example.com", "Nested": {"x": 1}, "deliveryAddress": {"street": "Testvej 1"}, "DoorCode": null}]`))
	o := v.([]any)[0].(obj)
	if o["Email"] != "kunde@example.com" || o["DoorCode"] != nil {
		t.Errorf("a real value was overwritten: %v", o)
	}
	addr := o["deliveryAddress"].(obj)
	if addr["street"] != "Testvej 1" || addr["city"] != "Andeby" {
		t.Errorf("addresses were not merged: %v", addr)
	}
	if o["Nested"].(obj)["CustomerName"] != "Anders And" || o["CustomerName"] != "Anders And" {
		t.Errorf("the leak is not in every object: %v", o)
	}
}

func TestCanaryNothingLeaksThroughTheReadMethods(t *testing.T) {
	isolate(t)
	t.Setenv("HANDLE_NEMLIG_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_NEMLIG_PASSWORD", "hemmeligt")
	h := &harness{tb: t, f: newFake(t), jar: newTestJar()}
	s := New(&http.Client{Transport: leaky{h.f}, Jar: h.jar})

	// The canary is really in the answers the adapter reads.
	basket, _, err := s.send(context.Background(), call{method: "GET", url: base + "/webapi/v2/Delivery/GetDeliveryDays"})
	if err != nil || !strings.Contains(jsonText(basket), "Anders And") {
		t.Fatalf("the canary is not in the fixtures (%v)", err)
	}

	ctx := context.Background()
	four := 4
	reads := map[string]func() (any, error){
		"search":   func() (any, error) { return s.Search(ctx, "cocio", 5) },
		"offers":   func() (any, error) { return s.Offers(ctx, 10) },
		"recipes":  func() (any, error) { return s.Recipes(ctx, "kylling karry", 3) },
		"recipe":   func() (any, error) { return s.Recipe(ctx, "kebabfad-med-fladbrod-98001303", &four) },
		"featured": func() (any, error) { return s.FeaturedRecipes(ctx, 20) },
		"by_tag":   func() (any, error) { return s.RecipesByTag(ctx, []string{"Familievenlig"}, nil, 5) },
		"cart":     func() (any, error) { return s.GetCart(ctx) },
		"terms":    func() (any, error) { return s.Terms(ctx) },
		"info":     func() (any, error) { return s.DeliveryInfo(ctx) },
		"slots":    func() (any, error) { return s.DeliverySlots(ctx, 3) },
		"orders":   func() (any, error) { return s.Orders(ctx, 5) },
		"order":    func() (any, error) { return s.Order(ctx, "900001") },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			result, err := read()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := privacy.Out(result)
			if err != nil || len(out) < 20 {
				t.Fatalf("%s: no answer (%v)", name, err)
			}
			for _, secret := range leakValues() {
				if strings.Contains(out, secret) {
					t.Errorf("%s leaks %q:\n%s", name, secret, out)
				}
			}
		})
	}
}
