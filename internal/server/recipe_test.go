package server

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func TestAddRecipeReportsBeforeAddedAndAfter(t *testing.T) {
	factories, _, _ := standard()
	a := obj(t, connect(t, factories).call("add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1"}).Text)
	line := a["added"].([]any)[0].(map[string]any)
	got := []any{line["before"], line["added"], line["after"], line["ok"]}
	want := []any{decode(t, "2"), decode(t, "2"), decode(t, "4"), true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("before/added/after/ok %v", got)
	}
	if a["skipped"] == nil || len(a["skipped"].([]any)) != 0 {
		t.Errorf("skipped %v", a["skipped"])
	}
	if cart := a["cart"].(map[string]any)["lines"].([]any); fmt.Sprint(cart[0].(map[string]any)["quantity"]) != "4" {
		t.Errorf("cart %v", cart)
	}
}

func TestAddRecipeSkippedAndRefusedLines(t *testing.T) {
	factories, fake, _ := standard()
	s := connect(t, factories)
	res := s.call("add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1", "skip": []string{"Mælk"}})
	if !res.IsError || res.Text != "Ingen varer at lægge i kurven." {
		t.Errorf("%v %q", res.IsError, res.Text)
	}
	// A store can refuse a line: ok false and the applied count, which the model must tell the human.
	fake.setFn = func(items []store.CartItem) ([]model.QuantityResult, error) {
		return []model.QuantityResult{{ProductID: "1", Requested: 4, Applied: ptr(3), OK: false, Message: ptr("Højst 3 pr. ordre")}}, nil
	}
	line := obj(t, s.call("add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1"}).Text)["added"].([]any)[0].(map[string]any)
	if line["ok"] != false || line["message"] != "Højst 3 pr. ordre" || fmt.Sprint(line["after"]) != "3" {
		t.Errorf("%v", line)
	}
}

func TestAddRecipeRefusesMoreThanTheLimitPerProductAndWritesNothing(t *testing.T) {
	factories, fake, _ := standard()
	fake.cart[0].Quantity = 49 // the recipe adds 2
	var written bool
	fake.setFn = func([]store.CartItem) ([]model.QuantityResult, error) { written = true; return nil, nil }
	res := connect(t, factories).call("add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1"})
	if !res.IsError || res.Text != "Højst 50 stk. pr. vare. For mange af: 1." || written {
		t.Errorf("isError %v text %q written %v", res.IsError, res.Text, written)
	}
}

func TestAddRecipePlanErrorsAreTheToolsOwnText(t *testing.T) {
	factories, _, _ := standard()
	s := connect(t, factories)
	for _, c := range []struct {
		args map[string]any
		text string
	}{
		{map[string]any{"include": []string{"Peber"}}, "Opskriften har ikke 'Peber'. Ingredienser: mælk."},
		{map[string]any{"swap": map[string]string{"mælk": "999"}}, "999 er ikke et alternativ til mælk: ingen."},
	} {
		c.args["store"], c.args["recipe"] = "fake", "r1"
		if res := s.call("add_recipe_to_cart", c.args); !res.IsError || res.Text != c.text {
			t.Errorf("%q, want %q", res.Text, c.text)
		}
	}
}

func TestGetRecipeClampsPersons(t *testing.T) {
	factories, fake, _ := standard()
	fake.rec = &recorder{}
	s := connect(t, factories)
	for _, p := range []any{1, 99, nil, 6} {
		s.call("get_recipe", map[string]any{"store": "fake", "recipe": "r1", "persons": p})
	}
	if want := []string{"persons=2", "persons=12", "persons=None", "persons=6"}; !reflect.DeepEqual(fake.rec.all(), want) {
		t.Errorf("%v", fake.rec.all())
	}
}

func TestListRecipesFeaturedOrByTag(t *testing.T) {
	factories, _, _ := standard()
	s := connect(t, factories)
	name := func(args map[string]any) any {
		args["stores"] = []string{"fake"}
		return obj(t, s.call("list_recipes", args).Text)["fake"].([]any)[0].(map[string]any)["name"]
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "Grød"}, // featured
		{map[string]any{"tags": []string{}}, "Grød"},
		{map[string]any{"category": ""}, "Grød"},
		{map[string]any{"tags": []string{"Familievenlig"}}, "Pasta"},
		{map[string]any{"category": "frokost"}, "Pasta"},
	} {
		if got := name(c.args); got != c.want {
			t.Errorf("%v: %v, want %v", c.args, got, c.want)
		}
	}
}

func TestLimitsAreClamped(t *testing.T) {
	factories, fake, _ := standard()
	fake.rec = &recorder{}
	s := connect(t, factories)
	s.call("search_products", map[string]any{"query": "x", "stores": []string{"fake"}, "limit": 999})
	s.call("get_orders", map[string]any{"store": "fake", "limit": 0})
	s.call("list_delivery_slots", map[string]any{"stores": []string{"fake"}, "days": 99})
	s.call("list_delivery_slots", map[string]any{"stores": []string{"fake"}, "days": 0})
	got := strings.Join(fake.rec.all(), "|")
	if got != "fake q='x' limit=25|limit=1|days=14|days=1" {
		t.Errorf("%s", got)
	}
}

func TestAPanicInADeliveryCallIsAnAnswerNotACrash(t *testing.T) {
	fake := newFake("fake")
	res := connect(t, map[string]Factory{"x": func() store.Store { return panicStore{fake} }}).call("get_delivery_info", map[string]any{"store": "x"})
	if !res.IsError || !strings.HasPrefix(res.Text, "x: uventet fejl (string)") {
		t.Errorf("%v %q", res.IsError, res.Text)
	}
}

// panicStore panics in DeliveryInfo: the tool must answer, not crash the server.
type panicStore struct{ *fakeStore }

func (panicStore) DeliveryInfo(context.Context) (model.DeliveryInfo, error) { panic("boom") }
