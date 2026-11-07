package rema1000

// ADR 0005, privacy test 2: personal data an adapter could leak by mistake. The fake adds the LEAK
// map of upstream/tests/conftest.py to every JSON object it answers with (merging, not overwriting).
// The read methods run through the real adapter, the results go through privacy.Out, and no LEAK
// value may be in the output. The user fields REMA sends (email, phone, name, birthday, photo) and
// a store's phone and address are in the leak too.

import (
	"context"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func leak() obj {
	return obj{
		"CustomerName": "Anders And", "Email": "anders@example.com", "MobileNumber": "+45 98 76 54 32",
		"DoorCode": "kode-4711", "UnattendedNotes": "Nøglen ligger under måtten",
		"email": "anders@example.com", "phone": "+45 98 76 54 32", "birthday": "1934-06-09",
		"photo": "https://example.com/anders.jpg", "address": "Andeby 1", "postal_code": "postnr-9999",
		"deliveryAddress": obj{"street": "Andeby 1", "zip": "postnr-9999", "city": "Andeby"},
	}
}

func leakValues() []string {
	return []string{"Anders And", "anders@example.com", "+45 98 76 54 32", "kode-4711", "Nøglen ligger under måtten",
		"1934-06-09", "anders.jpg", "Andeby 1", "postnr-9999"}
}

// leakAll puts the leak into every object of v. A key that is already there stays.
func leakAll(v any) any {
	switch x := v.(type) {
	case obj:
		for k, e := range x {
			x[k] = leakAll(e)
		}
		for k, e := range leak() {
			if _, there := x[k]; !there {
				x[k] = e
			}
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = leakAll(e)
		}
	case []obj:
		for i, e := range x {
			x[i] = leakAll(e).(obj)
		}
	}
	return v
}

func TestCanaryNothingLeaksThroughTheReadMethods(t *testing.T) {
	f, s := harness(t)
	f.leak = true
	f.addList("handle-mcp 2026-11-07", false, fakeItem{ID: 10, StoreItemID: 401042, Amount: 2})
	ctx := context.Background()
	six := 6
	reads := map[string]func() (any, error){
		"search":   func() (any, error) { return s.Search(ctx, "mælk", 5) },
		"offers":   func() (any, error) { return s.Offers(ctx, 5) },
		"recipes":  func() (any, error) { return s.Recipes(ctx, "kylling", 3) },
		"recipe":   func() (any, error) { return s.Recipe(ctx, "kylling-i-tarteletter", &six) },
		"featured": func() (any, error) { return s.FeaturedRecipes(ctx, 3) },
		"by_tag":   func() (any, error) { return s.RecipesByTag(ctx, []string{"Avisopskrifter"}, nil, 3) },
		"cart":     func() (any, error) { return s.GetCart(ctx) },
		"terms":    func() (any, error) { return s.Terms(ctx) },
		"info":     func() (any, error) { return s.DeliveryInfo(ctx) },
		"slots":    func() (any, error) { return s.DeliverySlots(ctx, 3) },
		"set":      func() (any, error) { return s.SetQuantities(ctx, []store.CartItem{{ProductID: "401042", Quantity: 3}}) },
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
	// the canary is really in what the fake sends
	choices := map[string]bool{}
	_, list, err := s.Choices(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		choices[c.Label] = true
		for _, secret := range leakValues() {
			if strings.Contains(c.Label, secret) {
				t.Errorf("a store choice leaks %q", secret)
			}
		}
	}
	if len(choices) == 0 {
		t.Error("no store to choose from")
	}
}
