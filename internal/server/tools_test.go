package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// Ports of upstream/tests/test_tools.py and the behaviour of server.py. The mapping table is in the report.

func obj(t testing.TB, text string) map[string]any {
	t.Helper()
	m, ok := decode(t, text).(map[string]any)
	if !ok {
		t.Fatalf("not an object: %s", text)
	}
	return m
}

func TestSearchOneBrokenStoreDoesNotStopTheOthers(t *testing.T) {
	factories, _, _ := standard()
	res := connect(t, factories).call("search_products", map[string]any{"query": "mælk"})
	a := obj(t, res.Text)
	hit := a["fake"].([]any)[0].(map[string]any)
	if hit["name"] != "mælk 1 l" || hit["price"] != "12.95" {
		t.Errorf("fake: %v", hit)
	}
	if !strings.Contains(a["broken"].(map[string]any)["error"].(string), "500") {
		t.Errorf("broken: %v", a["broken"])
	}
	for _, key := range []string{"before_price", "offer_until"} { // null when the store does not say
		if v, ok := hit[key]; !ok || v != nil {
			t.Errorf("%s: %v, %v", key, v, ok)
		}
	}
}

func TestUnknownStoreIsAClearError(t *testing.T) {
	factories, _, _ := standard()
	res := connect(t, factories).call("search_products", map[string]any{"query": "mælk", "stores": []string{"netto"}})
	if !res.IsError || res.Text != "Ukendt butik 'netto'. Kendte butikker: broken, fake." {
		t.Errorf("%v %q", res.IsError, res.Text)
	}
	res = connect(t, map[string]Factory{}).call("get_cart", map[string]any{"store": "x"})
	if res.Text != "Ukendt butik 'x'. Kendte butikker: ingen endnu." {
		t.Errorf("%q", res.Text)
	}
	if res = connect(t, map[string]Factory{}).call("get_cart", nil); res.Text != "Ingen butikker er sat op endnu." {
		t.Errorf("%q", res.Text)
	}
}

func TestNotImplementedAnswersPolitely(t *testing.T) {
	factories, _, _ := standard()
	res := connect(t, factories).call("get_orders", map[string]any{"store": "broken"})
	if !res.IsError || !strings.Contains(res.Text, "kan ikke det endnu") {
		t.Errorf("%v %q", res.IsError, res.Text)
	}
}

func TestSetCartQuantitiesReturnsResultsAndNewCart(t *testing.T) {
	factories, _, _ := standard()
	a := obj(t, connect(t, factories).call("set_cart_quantities",
		map[string]any{"store": "fake", "items": map[string]int{"1": 0, "2": 3}}).Text)
	var applied []any
	for _, r := range a["results"].([]any) {
		applied = append(applied, r.(map[string]any)["applied"])
	}
	if !reflect.DeepEqual(applied, []any{decode(t, "0"), decode(t, "3")}) {
		t.Errorf("applied %v", applied)
	}
	lines := a["cart"].(map[string]any)["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["product_id"] != "2" {
		t.Errorf("cart %v", lines)
	}
}

func TestSetCartQuantitiesKeepsTheOrderOfTheRequest(t *testing.T) {
	factories, fake, _ := standard()
	var seen []string
	fake.setFn = func(items []store.CartItem) ([]model.QuantityResult, error) {
		for _, it := range items {
			seen = append(seen, it.ProductID)
		}
		return nil, nil
	}
	connect(t, factories).call("set_cart_quantities", json.RawMessage(`{"store":"fake","items":{"9":1,"3":1,"5":1,"3":2}}`))
	if !reflect.DeepEqual(seen, []string{"9", "3", "5"}) { // a repeated key keeps its place
		t.Errorf("order %v", seen)
	}
}

func TestSetCartQuantitiesRejectsBadInput(t *testing.T) {
	factories, fake, _ := standard()
	s := connect(t, factories)
	many := map[string]int{}
	for i := 0; i < 51; i++ {
		many[string(rune('a'+i%26))+string(rune('a'+i/26))] = 1
	}
	for _, c := range []struct {
		items map[string]int
		text  string
	}{
		{map[string]int{}, "Ingen varer angivet."},
		{map[string]int{"1": -1}, "Antal skal være 0-50. Forkert for: 1."},
		{map[string]int{"1": 51}, "Antal skal være 0-50. Forkert for: 1."},
		{many, "Højst 50 varer pr. kald."},
	} {
		res := s.call("set_cart_quantities", map[string]any{"store": "fake", "items": c.items})
		if !res.IsError || res.Text != c.text {
			t.Errorf("%v: %q, want %q", c.items, res.Text, c.text)
		}
	}
	if len(fake.cart) != 1 || fake.cart[0].Quantity != 2 {
		t.Errorf("the cart changed: %v", fake.cart)
	}
}

func TestAnswersAreJSONWithMoneyAsText(t *testing.T) {
	factories, _, _ := standard()
	a := obj(t, connect(t, factories).call("get_delivery_info", map[string]any{"store": "fake"}).Text)
	if a["delivery"].(map[string]any)["chosen_start"] != "2026-11-03T16:00:00" || a["terms"].(map[string]any)["minimum_order"] != "500.00" {
		t.Errorf("%v", a)
	}
}

func TestRecipesInAStoreWithoutRecipesDoNotStopTheOthers(t *testing.T) {
	factories, _, broken := standard()
	broken.recipesFn = func(string, int) ([]model.Recipe, error) { return nil, store.ErrNotImplemented }
	a := obj(t, connect(t, factories).call("search_recipes", map[string]any{"query": "karry"}).Text)
	if a["fake"].([]any)[0].(map[string]any)["name"] != "karry til 4" || a["broken"].(map[string]any)["error"] != "broken kan ikke det endnu." {
		t.Errorf("%v", a)
	}
}

func TestDeliverySlotsInTimeOrderCheapestFirstOnlyFree(t *testing.T) {
	factories, _, _ := standard()
	fake := obj(t, connect(t, factories).call("list_delivery_slots", map[string]any{"stores": []string{"fake"}}).Text)["fake"].(map[string]any)
	prices := func(key string) (out []any) {
		for _, s := range fake[key].([]any) {
			out = append(out, s.(map[string]any)["price"])
		}
		return out
	}
	if want := []any{"25.00", "19.00", "0.00"}; !reflect.DeepEqual(prices("slots"), want) { // the sold-out slot is left out
		t.Errorf("slots %v", prices("slots"))
	}
	if want := []any{"0.00", "19.00", "25.00"}; !reflect.DeepEqual(prices("cheapest"), want) {
		t.Errorf("cheapest %v", prices("cheapest"))
	}
	if first := fake["cheapest"].([]any)[0].(map[string]any); first["start"] != "2026-11-04T14:00:00" {
		t.Errorf("start %v", first["start"])
	}
}

func TestCheapestSlotsAreThreeAndAnUnknownPriceComesLast(t *testing.T) {
	factories, fake, _ := standard()
	var slots []model.DeliverySlot
	for i, price := range []*model.Money{nil, ptr(kr(900)), ptr(kr(500)), ptr(kr(700)), ptr(kr(500))} {
		s := model.NewDeliverySlot("fake", naive(2026, 11, 3+i, 10), naive(2026, 11, 3+i, 12), price)
		slots = append(slots, s)
	}
	fake.slotsFn = func() []model.DeliverySlot { return slots }
	a := obj(t, connect(t, factories).call("list_delivery_slots", map[string]any{"stores": []string{"fake"}}).Text)["fake"].(map[string]any)
	var got []any
	for _, s := range a["cheapest"].([]any) {
		m := s.(map[string]any)
		got = append(got, m["price"], m["start"])
	}
	want := []any{"5.00", "2026-11-05T10:00:00", "5.00", "2026-11-07T10:00:00", "7.00", "2026-11-06T10:00:00"}
	if !reflect.DeepEqual(got, want) { // equal prices: the earlier first
		t.Errorf("cheapest %v", got)
	}
	if n := len(a["slots"].([]any)); n != 5 {
		t.Errorf("%d slots", n)
	}
	if last := a["slots"].([]any)[0].(map[string]any); last["price"] != nil {
		t.Errorf("the first slot has no price: %v", last)
	}
}

func products(prices ...[3]string) func(string, int) ([]model.Product, error) {
	return func(string, int) ([]model.Product, error) {
		var out []model.Product
		for _, p := range prices {
			m, _ := model.ParseMoney(p[1])
			x := model.NewProduct("fake", p[0], strings.ToUpper(p[0]), m)
			if p[2] != "" {
				u, _ := model.ParseMoney(p[2])
				x.UnitPrice = &u
			}
			out = append(out, x)
		}
		return out, nil
	}
}

func ids(t testing.TB, text string) (out []string) {
	for _, p := range obj(t, text)["fake"].([]any) {
		out = append(out, p.(map[string]any)["id"].(string))
	}
	return out
}

func TestSearchSortPutsUnknownUnitPriceLast(t *testing.T) {
	factories, fake, _ := standard()
	fake.searchFn = products([3]string{"a", "30", ""}, [3]string{"b", "20", "80"}, [3]string{"c", "10", "90"})
	s := connect(t, factories)
	for sort, want := range map[string][]string{"unit_price": {"b", "c", "a"}, "price": {"c", "b", "a"}, "relevance": {"a", "b", "c"}} {
		res := s.call("search_products", map[string]any{"query": "kylling", "stores": []string{"fake"}, "limit": 3, "sort": sort})
		if got := ids(t, res.Text); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", sort, got, want)
		}
	}
	res := s.call("search_products", map[string]any{"query": "kylling", "stores": []string{"fake"}, "limit": 3}) // the store's own order
	if got := ids(t, res.Text); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("default %v", got)
	}
}

func TestSearchByPricePutsAFreeGiftFirst(t *testing.T) { // on purpose: a free gift is the cheapest
	factories, fake, _ := standard()
	fake.searchFn = products([3]string{"bread", "25", "25"}, [3]string{"gift", "0", "0"})
	res := connect(t, factories).call("search_products", map[string]any{"query": "brød", "stores": []string{"fake"}, "limit": 2, "sort": "price"})
	if got := ids(t, res.Text); !reflect.DeepEqual(got, []string{"gift", "bread"}) {
		t.Errorf("%v", got)
	}
}

func TestGetCartWithoutAStoreGivesEveryCart(t *testing.T) {
	factories, _, broken := standard()
	broken.cartFn = func() (model.Cart, error) { return model.Cart{}, store.Errorf("Intet login til broken.") }
	carts := obj(t, connect(t, factories).call("get_cart", nil).Text)
	fake := carts["fake"].(map[string]any)
	if len(carts) != 2 || fake["store"] != "fake" || fake["lines"] == nil {
		t.Errorf("%v", carts)
	}
	if !strings.Contains(carts["broken"].(map[string]any)["error"].(string), "Intet login") {
		t.Errorf("broken: %v", carts["broken"]) // one store failing never stops the other
	}
}

func TestGetCartWithAnEmptyStoreNameIsAnError(t *testing.T) {
	factories, _, _ := standard()
	s := connect(t, factories)
	if res := s.call("get_cart", map[string]any{"store": ""}); !res.IsError || res.Text != "Ukendt butik ''. Kendte butikker: broken, fake." {
		t.Errorf("%q", res.Text)
	}
	if cart := obj(t, s.call("get_cart", map[string]any{"store": "fake"}).Text); cart["store"] != "fake" || cart["lines"] == nil {
		t.Errorf("%v", cart)
	}
}

func TestStoreNamedTwiceIsAskedOnce(t *testing.T) {
	factories, fake, _ := standard()
	fake.rec = &recorder{}
	connect(t, factories).call("list_offers", map[string]any{"stores": []string{"fake", "fake"}})
	if n := len(fake.rec.all()); n != 2 { // one "offers" line and one search line
		t.Errorf("%d calls: %v", n, fake.rec.all())
	}
}
