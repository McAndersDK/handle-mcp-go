package bilkatogo

// Pickup (docs/adr/0006): the three delivery modes, the fail-closed name rule, the pickup slots and
// fee, and the canary that a customer's name never leaves. The pickup fixtures are cart_pickup.json
// and available_delivery_dates_pickup.json (a fictional store, "Bilka Eksempelby").

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

const pickupStoreName = "Bilka Eksempelby"

// setCart loads a cart fixture. cart_pickup.json masks `messages` as the text "X"; a real cart has
// a list, so the test sets the empty list the live answer had.
func setCart(h *harness, name string) {
	h.f.cart = fixture(h.tb, name).(obj)
	if name == "cart_pickup" {
		h.f.cart["messages"] = []any{}
	}
}

// blankedHome is the home cart with its home markers blanked: the customer's name is still in
// deliveryAddress.name, and nothing says home or pickup.
func blankedHome(h *harness) {
	setCart(h, "cart_home")
	addr := h.f.cart["deliveryAddress"].(obj)
	addr["dawa_uid"], addr["recipient_name"] = "", ""
}

func TestModeAndStoreFromTheThreeCartFixtures(t *testing.T) {
	for _, c := range []struct {
		fixture, mode, store string // store "-" is nil
	}{
		{"cart_pickup", "pickup", pickupStoreName},
		{"cart_logged_in", "pickup", pickupStoreName},
		{"cart_home", "home", "-"},
	} {
		h := envLogin(t)
		setCart(h, c.fixture)
		cart, err := h.s.GetCart(bg)
		if err != nil {
			t.Fatal(err)
		}
		terms, err := h.s.Terms(bg)
		if err != nil {
			t.Fatal(err)
		}
		info, err := h.s.DeliveryInfo(bg)
		if err != nil {
			t.Fatal(err)
		}
		for what, got := range map[string][2]string{
			"cart":  {show(cart.DeliveryMode), show(cart.PickupStore)},
			"terms": {show(terms.DeliveryMode), show(terms.PickupStore)},
			"info":  {show(info.DeliveryMode), show(info.PickupStore)},
		} {
			if got[0] != c.mode || got[1] != c.store {
				t.Errorf("%s of %s: mode %q store %q, want %q %q", what, c.fixture, got[0], got[1], c.mode, c.store)
			}
		}
		if terms.HomeDelivery != (c.mode == "home") {
			t.Errorf("%s: home_delivery %v", c.fixture, terms.HomeDelivery)
		}
	}
}

func TestPickupCartHasNoHomeDeliveryWarning(t *testing.T) {
	h := envLogin(t)
	setCart(h, "cart_pickup")
	cart, err := h.s.GetCart(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(cart.Warnings) != 0 {
		t.Errorf("warnings on a pickup cart: %v", cart.Warnings)
	}
	if got := cart.Fees["pakkeservice"].String(); got != "39.00" {
		t.Errorf("Pakkeservice stays in the cart's fees, got %s", got)
	}
	if cart.Total.String() != "39.00" {
		t.Errorf("total %s", cart.Total)
	}
}

func TestUnknownModeFailsClosedAndTheNameNeverLeaves(t *testing.T) {
	h := envLogin(t)
	blankedHome(h)
	if got := deliveryMode(readAddress(h.f.cart)); got != modeUnknown {
		t.Fatalf("mode %d, want unknown", got)
	}
	cart, err := h.s.GetCart(bg)
	if err != nil {
		t.Fatal(err)
	}
	terms, err := h.s.Terms(bg)
	if err != nil {
		t.Fatal(err)
	}
	info, err := h.s.DeliveryInfo(bg)
	if err != nil {
		t.Fatal(err)
	}
	_, slotsErr := h.s.DeliverySlots(bg, 3)
	if storeErr(t, slotsErr) != notKnown {
		t.Errorf("slots error: %v", slotsErr)
	}
	for what, p := range map[string]*string{"cart mode": cart.DeliveryMode, "cart store": cart.PickupStore,
		"terms mode": terms.DeliveryMode, "terms store": terms.PickupStore,
		"info mode": info.DeliveryMode, "info store": info.PickupStore} {
		if p != nil {
			t.Errorf("%s is %q, want null", what, *p)
		}
	}
	if len(cart.Warnings) == 0 || cart.Warnings[0] != notKnown || info.Note == nil || *info.Note != notKnown {
		t.Errorf("warnings %v, note %s", cart.Warnings, show(info.Note))
	}
	if terms.HomeDelivery {
		t.Error("unknown is not home delivery")
	}
	text := slotsErr.Error()
	for _, v := range []any{cart, terms, info} {
		out, err := privacy.Out(v)
		if err != nil {
			t.Fatal(err)
		}
		text += out
	}
	if strings.Contains(text, "Kunde Eksempel") || strings.Contains(text, "Testvej") {
		t.Errorf("the customer's name or street got out:\n%s", text)
	}
}

func TestHomeCartNeverShowsAPickupStore(t *testing.T) {
	h := envLogin(t)
	setCart(h, "cart_home")
	h.f.cart["deliveryAddress"].(obj)["name"] = "Bilka Kunde" // a customer who happens to be named so
	cart, _ := h.s.GetCart(bg)
	if show(cart.DeliveryMode) != "home" || cart.PickupStore != nil {
		t.Errorf("mode %s store %s", show(cart.DeliveryMode), show(cart.PickupStore))
	}
	slots, err := h.s.DeliverySlots(bg, 3)
	if err != nil || len(slots) == 0 {
		t.Fatalf("%d slots, %v", len(slots), err)
	}
	for _, sl := range slots {
		if sl.PickupStore != nil || show(sl.Kind) != "Nat" {
			t.Errorf("home slot: kind %s store %s", show(sl.Kind), show(sl.PickupStore))
		}
	}
}

func TestPickupSlotsFromTheCapturedDates(t *testing.T) {
	h := envLogin(t)
	setCart(h, "cart_pickup")
	bodyValue(pAvail, fixture(t, "available_delivery_dates_pickup"))(h)
	slots, err := h.s.DeliverySlots(bg, 7)
	if err != nil {
		t.Fatalf("a pickup cart is no error any more: %v", err)
	}
	if len(slots) != 0 { // the capture was trimmed to three days with no intervals
		t.Errorf("%d slots from a capture with no intervals", len(slots))
	}
}

func TestPickupSlotsAreMarkedAndPriced(t *testing.T) {
	ivs := [][2]string{{"00:00", "06:00"}, {"08:00", "10:00"}, {"18:00", "22:00"}}
	h := envLogin(t)
	setCart(h, "cart_pickup")
	daysAt("2026-11-03", ivs)(h)
	slots, err := h.s.DeliverySlots(bg, 7)
	if err != nil || len(slots) != 3 {
		t.Fatalf("%d slots, %v", len(slots), err)
	}
	for _, sl := range slots {
		if show(sl.Kind) != "Afhentning" || show(sl.PickupStore) != pickupStoreName {
			t.Errorf("kind %s store %s", show(sl.Kind), show(sl.PickupStore))
		}
		if show(sl.Price) != "0.00" || !sl.Available {
			t.Errorf("price %s available %v", show(sl.Price), sl.Available)
		}
		if sl.Note == nil || strings.Contains(*sl.Note, "Gebyret er ukendt") {
			t.Errorf("note %s", show(sl.Note))
		}
	}
}

func TestPickupFeeZeroIsKnownAndAbsentIsNot(t *testing.T) {
	ivs := [][2]string{{"08:00", "10:00"}}
	for _, c := range []struct {
		name  string
		stat  string // JSON merged into stat; "-" removes delivery_price
		price string
		fee   string
		note  bool
	}{
		{"zero", `{"delivery_price": 0}`, "0.00", "0.00", false},
		{"absent", "-", "-", "-", true},
		{"null", `{"delivery_price": null}`, "-", "-", true},
		{"twenty-five", `{"delivery_price": 2500}`, "25.00", "25.00", false},
	} {
		h := envLogin(t)
		setCart(h, "cart_pickup")
		stat := h.f.cart["stat"].(obj)
		if c.stat == "-" {
			delete(stat, "delivery_price")
		} else {
			for k, v := range J(t, c.stat).(obj) {
				stat[k] = v
			}
		}
		daysAt("2026-11-03", ivs)(h)
		slots, err := h.s.DeliverySlots(bg, 7)
		if err != nil || len(slots) != 1 {
			t.Fatalf("%s: %d slots, %v", c.name, len(slots), err)
		}
		terms, err := h.s.Terms(bg)
		if err != nil {
			t.Fatal(err)
		}
		if show(slots[0].Price) != c.price || show(terms.DeliveryFee) != c.fee {
			t.Errorf("%s: slot price %s, terms fee %s", c.name, show(slots[0].Price), show(terms.DeliveryFee))
		}
		if got := slots[0].Note != nil && strings.HasPrefix(*slots[0].Note, "Gebyret er ukendt"); got != c.note {
			t.Errorf("%s: unknown-fee note %v, want %v", c.name, got, c.note)
		}
	}
}

// A home cart with a delivery_price of 0 keeps its old answer: the fee is unknown (home is byte
// for byte as before).
func TestHomeZeroFeeIsStillUnknown(t *testing.T) {
	h := envLogin(t)
	setCart(h, "cart_home")
	h.f.cart["stat"].(obj)["delivery_price"] = json.Number("0")
	slots, err := h.s.DeliverySlots(bg, 3)
	if err != nil || len(slots) == 0 {
		t.Fatalf("%d slots, %v", len(slots), err)
	}
	if slots[0].Price != nil || slots[0].Note == nil || !strings.HasPrefix(*slots[0].Note, "Gebyret er ukendt") {
		t.Errorf("price %s note %s", show(slots[0].Price), show(slots[0].Note))
	}
}

func TestStalePickupSlotIsHiddenAndAFutureOneShown(t *testing.T) {
	h := envLogin(t)
	setCart(h, "cart_pickup") // chosen slot: 2024-06-19, long ago
	cart, _ := h.s.GetCart(bg)
	info, err := h.s.DeliveryInfo(bg)
	if err != nil {
		t.Fatal(err)
	}
	if cart.Delivery != nil || info.ChosenStart != nil || info.ChosenEnd != nil {
		t.Errorf("a slot from 2024 is no choice: %s %s", show(cart.Delivery), show(info.ChosenStart))
	}
	if show(info.PickupStore) != pickupStoreName || info.Note != nil {
		t.Errorf("store %s note %s", show(info.PickupStore), show(info.Note))
	}
	h.f.cart["deliveryDate"] = J(t, `{"deliveryDate": "2026-11-05", "intervalStart": "10:00", "intervalEnd": "12:00",
	  "delivery_message": "Tor. 05/11. kl. 10:00 - 12:00"}`)
	cart, _ = h.s.GetCart(bg)
	info, _ = h.s.DeliveryInfo(bg)
	if show(cart.Delivery) != "Tor. 05/11. kl. 10:00 - 12:00" || info.ChosenStart == nil {
		t.Errorf("a coming slot is shown: %s %s", show(cart.Delivery), show(info.ChosenStart))
	}
}

// The canary over the pickup fixtures: personal data injected into every object (and the customer
// values of the home fixture) must not get out. The store name is the one thing that is meant to.
func TestCanaryOverThePickupFixtures(t *testing.T) {
	h := envLogin(t)
	h.s = New(&http.Client{Transport: leaky{h.f}}, WithClock(func() time.Time { return h.clock }))
	setCart(h, "cart_pickup")
	bodyValue(pAvail, fixture(t, "available_delivery_dates_pickup"))(h)
	daysAt("2026-11-03", [][2]string{{"08:00", "10:00"}})(h)
	cart, err1 := h.s.GetCart(bg)
	terms, err2 := h.s.Terms(bg)
	info, err3 := h.s.DeliveryInfo(bg)
	slots, err4 := h.s.DeliverySlots(bg, 7)
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			t.Fatal(err)
		}
	}
	var secrets []string
	for _, v := range append(leakValues(), fixtureValues()...) {
		if v != "Eksempelby" { // part of the fictional store's name
			secrets = append(secrets, v)
		}
	}
	for what, v := range map[string]any{"cart": cart, "terms": terms, "info": info, "slots": slots} {
		out, err := privacy.Out(v)
		if err != nil || len(out) < 20 {
			t.Fatalf("%s: no answer (%v)", what, err)
		}
		for _, secret := range secrets {
			if strings.Contains(out, secret) {
				t.Errorf("%s leaks %q:\n%s", what, secret, out)
			}
		}
		for _, street := range []string{"Eksempelvej", "0000 "} {
			if strings.Contains(out, street) {
				t.Errorf("%s leaks the store address %q", what, street)
			}
		}
	}
	if !strings.Contains(jsonText(toPlain(t, info)), pickupStoreName) {
		t.Error("the pickup store name is missing from the delivery info")
	}
}

func TestNewKeysAreNeverOmitted(t *testing.T) {
	for _, v := range []any{model.Cart{}, model.StoreTerms{}, model.DeliveryInfo{}} {
		raw, _ := json.Marshal(v)
		for _, key := range []string{`"delivery_mode":null`, `"pickup_store":null`} {
			if !strings.Contains(string(raw), key) {
				t.Errorf("%T lacks %s: %s", v, key, raw)
			}
		}
	}
	raw, _ := json.Marshal(model.DeliverySlot{})
	if !strings.Contains(string(raw), `"pickup_store":null`) {
		t.Errorf("DeliverySlot lacks pickup_store: %s", raw)
	}
}
