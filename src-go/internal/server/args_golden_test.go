package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/goldentest"
)

type probe struct {
	Tool     string          `json:"tool"`
	Arg      string          `json:"arg"`
	Input    json.RawMessage `json:"input"`
	Accepted bool            `json:"accepted"`
	Received json.RawMessage `json:"received"`
	Error    string          `json:"error"`
	Answer   json.RawMessage `json:"answer"`
}

// refusedStrings are the strings Python read as a number and ADR 0005 refuses, with true and false.
// Each is a named exception to args.json.
var refusedStrings = map[string]bool{"2.0": true, "+2": true, "-2": true, "02": true, "2_0": true, "1_000": true}

var intArgs = map[string]bool{
	"get_orders.limit": true, "get_recipe.persons": true, "list_delivery_slots.days": true,
	"search_products.limit": true, "set_cart_quantities.items": true,
}

// refusedNumber reports whether the value (or one inside an object, or inside JSON text of an
// object) is one of the refused spellings of an integer.
func refusedNumber(v any) bool {
	switch x := v.(type) {
	case bool:
		return true
	case string:
		if refusedStrings[x] {
			return true
		}
		var inner map[string]any
		dec := json.NewDecoder(strings.NewReader(x))
		dec.UseNumber()
		if dec.Decode(&inner) == nil {
			return refusedNumber(inner)
		}
	case map[string]any:
		for _, e := range x {
			if refusedNumber(e) {
				return true
			}
		}
	}
	return false
}

// callFor builds the call for a probe: the probed argument next to the ones the tool needs.
func callFor(p probe) map[string]json.RawMessage {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	args := map[string]json.RawMessage{p.Arg: p.Input}
	set := func(k, v string) {
		if k != p.Arg {
			args[k] = raw(v)
		}
	}
	switch p.Tool {
	case "get_orders", "get_order", "set_cart_quantities":
		set("store", `"fake"`)
	case "get_recipe", "add_recipe_to_cart":
		set("store", `"fake"`)
		set("recipe", `"r1"`)
	case "list_delivery_slots", "compare_basket":
		set("stores", `["fake"]`)
	case "search_products":
		set("query", `"x"`)
		if p.Arg != "stores" {
			set("stores", `["fake"]`)
		}
	}
	return args
}

var queryOf = regexp.MustCompile(`^\w+ q='(.*)' limit=\d+$`)

// received is what the fake saw, in the shape args.json records it.
func received(p probe, lines []string) any {
	switch {
	case p.Arg == "stores":
		seen := map[string]bool{}
		for _, l := range lines {
			seen[strings.Fields(l)[0]] = true
		}
		var names []string
		for n := range seen {
			names = append(names, n)
		}
		sort.Strings(names)
		return names
	case p.Tool == "compare_basket":
		queries := []string{}
		for _, l := range lines {
			if m := queryOf.FindStringSubmatch(l); m != nil {
				queries = append(queries, m[1])
			}
		}
		return queries
	case len(lines) != 1:
		return fmt.Sprintf("%d calls: %v", len(lines), lines)
	case p.Tool == "search_products":
		_, rest, _ := strings.Cut(lines[0], " ")
		return rest
	}
	return lines[0]
}

func TestArgsGolden(t *testing.T) {
	var g struct {
		Probes []probe `json:"probes"`
	}
	goldentest.Read(t, "wire/args.json", &g)
	if len(g.Probes) != 466 {
		t.Fatalf("%d probes, want 466", len(g.Probes))
	}
	exceptions := map[string]int{}
	var accepted, sameText, sameArgument int
	for i, p := range g.Probes {
		label := fmt.Sprintf("probe %d %s.%s = %s", i, p.Tool, p.Arg, p.Input)
		factories, fake, broken := standard()
		fake.rec = &recorder{}
		broken.rec = fake.rec
		fake.noItems, broken.noItems = true, true
		res := connect(t, factories).call(p.Tool, callFor(p))
		if res.Err != nil {
			t.Errorf("%s: %v", label, res.Err)
			continue
		}
		input := decode(t, string(p.Input))
		lines := fake.rec.all()
		switch {
		case intArgs[p.Tool+"."+p.Arg] && refusedNumber(input): // named exception (ADR 0005)
			path := p.Arg
			if p.Tool == "set_cart_quantities" {
				path += ".1"
			}
			if want := "Argumentet '" + path + "' skal være et heltal."; !res.IsError || res.Text != want || len(lines) != 0 {
				t.Errorf("%s: isError %v text %q (store saw %v), want %q", label, res.IsError, res.Text, lines, want)
			}
			exceptions[p.Tool+"."+p.Arg]++
		case p.Accepted:
			accepted++
			if res.IsError {
				t.Errorf("%s: refused: %s", label, res.Text)
				continue
			}
			if p.Tool == "get_cart" { // store null: every cart
				var want string
				_ = json.Unmarshal(p.Answer, &want)
				if !reflect.DeepEqual(goldentest.DropAdded(decode(t, res.Text)), decode(t, want)) {
					t.Errorf("%s: answer differs from the golden:\n%s", label, res.Text)
				}
				continue
			}
			var want any
			_ = json.Unmarshal(p.Received, &want)
			if list, ok := want.([]any); ok { // a list of strings
				strs := []string{}
				for _, e := range list {
					strs = append(strs, e.(string))
				}
				want = strs
			}
			if p.Tool == "get_order" {
				want = "order_id=" + want.(string)
			}
			if got := received(p, lines); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the store saw %v, golden %v", label, got, want)
			}
		case strings.HasPrefix(p.Error, "1 validation error"):
			want := "Argumentet '" + loc(p.Error) + "'"
			if !res.IsError || !strings.HasPrefix(res.Text, want) || len(lines) != 0 {
				t.Errorf("%s: isError %v text %q (store saw %v), want it to start with %q", label, res.IsError, res.Text, lines, want)
			}
			sameArgument++
		default: // an error of the tool itself: the text is the same
			if !res.IsError || res.Text != p.Error {
				t.Errorf("%s: isError %v text %q, golden %q", label, res.IsError, res.Text, p.Error)
			}
			sameText++
		}
	}
	total := 0
	for _, n := range exceptions {
		total += n
	}
	want := map[string]int{"get_orders.limit": 8, "get_recipe.persons": 8, "list_delivery_slots.days": 8,
		"search_products.limit": 8, "set_cart_quantities.items": 10}
	if !reflect.DeepEqual(exceptions, want) || accepted != 133 || sameArgument != 255 || sameText != 36 {
		t.Errorf("exceptions %v (want %v), accepted %d (want 133), same argument %d (want 255), same text %d (want 36)",
			exceptions, want, accepted, sameArgument, sameText)
	}
	t.Logf("accepted %d, refused with the same argument %d, same tool error text %d, named exceptions %d %v",
		accepted, sameArgument, sameText, total, exceptions)
}
