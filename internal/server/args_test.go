package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var defs = loadTools()

func decodeFor(tool, args string) (Args, error) {
	return decodeArgs(defs[tool].params, []byte(args))
}

func TestIntegerRule(t *testing.T) {
	accepted := map[string]int{
		`2`: 2, `2.0`: 2, `2.00`: 2, `-1`: -1, `0`: 0, `-0`: 0, `"2"`: 2, `" 2 "`: 2, `"0"`: 0, `"2\n"`: 2, `"\t2"`: 2,
		`"999999999999999999999999999999"`: satur, `99999999999999999999`: satur, `-99999999999999999999`: -satur,
	}
	for in, want := range accepted {
		a, err := decodeFor("get_orders", `{"store":"s","limit":`+in+`}`)
		if err != nil || a.Int("limit") != want {
			t.Errorf("%s: %v %v, want %d", in, a, err, want)
		}
	}
	for _, in := range []string{
		`true`, `false`, `null`, `[]`, `{}`, `[2]`, `{"a":2}`, `""`, `" "`, `2.5`, `1e1`, `2e0`, `1e+30`,
		`"2_0"`, `"+2"`, `"02"`, `"2.0"`, `"-1"`, `"-0"`, `"٢"`, `"２"`, `"2e0"`, `"0x2"`, `"NaN"`, `"Infinity"`,
		`"mange"`, `"true"`, `"2,0"`, `"2 3"`, `"00"`, `"1_000"`, `" 2"`,
	} {
		a, err := decodeFor("get_orders", `{"store":"s","limit":`+in+`}`)
		if err == nil {
			t.Errorf("%s was accepted as %v", in, a)
			continue
		}
		// A clear Danish error that names the argument and never echoes the value.
		if want := "Argumentet 'limit' "; !strings.HasPrefix(err.Error(), want) || strings.ContainsAny(err.Error(), "0123456789_+") {
			t.Errorf("%s: %q", in, err)
		}
	}
}

func TestErrorsNeverEchoTheValue(t *testing.T) {
	for _, c := range []struct{ tool, args, text string }{
		{"get_orders", `{"store":"s","limit":"hemmeligt"}`, "Argumentet 'limit' skal være et heltal."},
		{"get_orders", `{"limit":3}`, "Argumentet 'store' mangler."},
		{"get_orders", `{"store":null}`, "Argumentet 'store' må ikke være null."},
		{"get_orders", `{"store":7}`, "Argumentet 'store' skal være tekst."},
		{"get_orders", `{"store":"s","limit":null}`, "Argumentet 'limit' må ikke være null."},
		{"search_products", `{"query":"x","sort":"hemmeligt"}`, "Argumentet 'sort' skal være en af: relevance, price, unit_price."},
		{"search_products", `{"query":"x","sort":null}`, "Argumentet 'sort' må ikke være null."},
		{"list_delivery_slots", `{"days":null}`, "Argumentet 'days' må ikke være null."},
		{"list_offers", `{"limit":null}`, "Argumentet 'limit' må ikke være null."},
		{"search_products", `{"query":"x","stores":"hemmeligt"}`, "Argumentet 'stores' skal være en liste."},
		{"search_products", `{"query":"x","stores":["a",7]}`, "Argumentet 'stores.1' skal være tekst."},
		{"set_cart_quantities", `{"store":"s","items":{"7":"hemmeligt"}}`, "Argumentet 'items.7' skal være et heltal."},
		{"set_cart_quantities", `{"store":"s","items":[1]}`, "Argumentet 'items' skal være et objekt."},
		{"add_recipe_to_cart", `{"store":"s","recipe":"r","swap":{"a":2}}`, "Argumentet 'swap.a' skal være tekst."},
		{"add_recipe_to_cart", `{"store":"s","recipe":"r","skip":{}}`, "Argumentet 'skip' skal være en liste."},
		{"get_cart", `[1]`, "Argumenterne skal være et JSON-objekt."},
		{"get_cart", `"x"`, "Argumenterne skal være et JSON-objekt."},
	} {
		if _, err := decodeFor(c.tool, c.args); err == nil || err.Error() != c.text {
			t.Errorf("%s %s: %v, want %q", c.tool, c.args, err, c.text)
		}
	}
}

func TestNullOnlyWhereTheSchemaAllowsIt(t *testing.T) {
	for _, c := range []struct{ tool, args, arg string }{
		{"get_cart", `{"store":null}`, "store"},
		{"get_recipe", `{"store":"s","recipe":"r","persons":null}`, "persons"},
		{"search_products", `{"query":"x","stores":null}`, "stores"},
		{"list_recipes", `{"tags":null,"category":null}`, "tags"},
		{"add_recipe_to_cart", `{"store":"s","recipe":"r","skip":null,"swap":null,"include":null}`, "swap"},
		{"search_products", `{"query":"x","stores":"null"}`, "stores"}, // JSON text that parses to null
	} {
		a, err := decodeFor(c.tool, c.args)
		if err != nil || a[c.arg] != nil {
			t.Errorf("%s %s: %v %v", c.tool, c.args, a, err)
		}
	}
}

func TestDefaultsUnknownKeysAndNoArguments(t *testing.T) {
	a, err := decodeFor("search_products", `{"query":"x","surprise":1}`)
	if err != nil || a.Int("limit") != 10 || a.Str("sort") != "relevance" || a.List("stores") != nil {
		t.Errorf("%v %v", a, err)
	}
	for _, raw := range []string{``, `null`, `{}`, ` `} {
		if a, err := decodeFor("list_delivery_slots", raw); err != nil || a.Int("days") != 7 {
			t.Errorf("%q: %v %v", raw, a, err)
		}
	}
	if _, err := decodeFor("search_products", ``); err == nil || err.Error() != "Argumentet 'query' mangler." {
		t.Errorf("a required argument is missing: %v", err)
	}
	for tool, want := range map[string]int{"search_recipes": 5, "list_recipes": 20, "list_offers": 25, "get_orders": 10} {
		args := `{"store":"s","query":"q"}`
		if a, err := decodeFor(tool, args); err != nil || a.Int("limit") != want {
			t.Errorf("%s limit %v %v", tool, a.Int("limit"), err)
		}
	}
}

func TestListsAndObjectsGivenAsJSONText(t *testing.T) {
	for _, in := range []string{`["a","b"]`, ` ["a","b"] `, "\t[\"a\",\"b\"]\n"} {
		raw := `{"query":"x","stores":` + jsonQuote(in) + `}`
		if a, err := decodeFor("search_products", raw); err != nil || !reflect.DeepEqual(a.List("stores"), []string{"a", "b"}) {
			t.Errorf("%s: %v %v", in, a, err)
		}
	}
	for _, in := range []string{`a`, `"a"`, `a,b`, `["a",]`, `['a']`, `["a"] x`, `{}`, `5`, `true`, ``} {
		if a, err := decodeFor("search_products", `{"query":"x","stores":`+jsonQuote(in)+`}`); err == nil {
			t.Errorf("%s was accepted as %v", in, a)
		}
	}
	a, err := decodeFor("set_cart_quantities", `{"store":"s","items":"{\"b\": \"2\", \"a\": 2.0}"}`)
	if err != nil || !reflect.DeepEqual(a.IntPairs("items"), []IntPair{{"b", 2}, {"a", 2}}) {
		t.Errorf("%v %v", a, err)
	}
	if _, err := decodeFor("set_cart_quantities", `{"store":"s","items":"{\"a\": true}"}`); err == nil {
		t.Error("true inside JSON text was accepted")
	}
}

func TestObjectsKeepTheOrderOfTheCaller(t *testing.T) {
	a, err := decodeFor("add_recipe_to_cart", `{"store":"s","recipe":"r","swap":{"z":"1","a":"2","m":"3","z":"4"}}`)
	want := []StrPair{{"z", "4"}, {"a", "2"}, {"m", "3"}} // a repeated key keeps its place and takes the last value
	if err != nil || !reflect.DeepEqual(a.StrPairs("swap"), want) {
		t.Errorf("%v %v", a.StrPairs("swap"), err)
	}
}

func TestEverySchemaIsUnderstood(t *testing.T) {
	if len(defs) != 13 {
		t.Fatalf("%d tools", len(defs))
	}
	for name, d := range defs {
		if d.Annotations == nil || d.Description == "" {
			t.Errorf("%s is incomplete", name)
		}
	}
	nullable := map[string]bool{}
	for _, p := range defs["add_recipe_to_cart"].params {
		nullable[p.name] = p.nullable
	}
	if want := map[string]bool{"store": false, "recipe": false, "persons": true, "skip": true, "swap": true, "include": true}; !reflect.DeepEqual(nullable, want) {
		t.Errorf("nullable %v", nullable)
	}
}

func jsonQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
