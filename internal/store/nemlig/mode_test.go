package nemlig

import (
	"context"
	"testing"
)

// nemlig is home delivery only (docs/adr/0006): the cart, terms and delivery info say "home", and
// there is never a pickup store. The goldens do not see these keys (goldentest.DropAdded).
func TestNemligIsHomeDeliveryWithNoPickupStore(t *testing.T) {
	_, s := envLogin(t)
	ctx := context.Background()
	cart, err := s.GetCart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	terms, err := s.Terms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.DeliveryInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]*string{"cart": cart.DeliveryMode, "terms": terms.DeliveryMode, "info": info.DeliveryMode}
	for what, m := range modes {
		if m == nil || *m != "home" {
			t.Errorf("%s delivery_mode is not \"home\"", what)
		}
	}
	if cart.PickupStore != nil || terms.PickupStore != nil || info.PickupStore != nil {
		t.Error("nemlig has no pickup store")
	}
}
