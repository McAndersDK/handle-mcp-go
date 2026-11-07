package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/goldentest"
)

func snapshotPath() string {
	return filepath.Join(goldentest.Path("."), "..", "snapshots", "tools.json")
}

// toolsOnTheWire is tools/list as a client sees it, sorted by name, as generic JSON.
func toolsOnTheWire(t testing.TB, s *session) []map[string]any {
	t.Helper()
	res, err := s.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, tool := range res.Tools {
		out = append(out, decode(t, string(jsonOf(t, tool))).(map[string]any))
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return out
}

// The four descriptions that change for pickup (ADR 0006), written out here so the test does not
// read them from tools.json.
const (
	cartAddition = " `delivery_mode` is \"home\", \"pickup\" or\nnull (the store does not say); " +
		"`pickup_store` is the store's name, only for pickup."
	compareAddition = "Each store has `delivery_mode` and `pickup_store`; a pickup store is noted, and when one store\n" +
		"delivers and another is pickup, the summary names the cheapest of each (the trip to the store\n" +
		"is not counted). "
	deliveryInfoText = "Show the chosen delivery or pickup time, edit deadline and next free day, plus fees and\n" +
		"minimum order. `delivery_mode` is \"home\", \"pickup\" or null (the store does not say: the human\n" +
		"chooses on the store's website); `pickup_store` is set only for pickup. Only reads: it never\n" +
		"chooses a time, a pickup store or home delivery or pickup."
	slotsText = "List free delivery or pickup times for the next `days` days with the fee per slot, in time\n" +
		"order, plus the 3 cheapest per store. nemlig: \"Personlig\" (you are home) or \"Fleksibel\"\n" +
		"(left at the door). BilkaToGo: home delivery has one flat fee and night delivery; a pickup\n" +
		"cart gives pickup times with kind \"Afhentning\", `pickup_store` and the fee (\"0.00\" when free).\n" +
		"Only reads: it never chooses a time, a pickup store or home delivery or pickup. The human\n" +
		"chooses on the store's website."
)

// adjusted is the Python snapshot with exactly the named adjustments of ADR 0005 and 0006:
// idempotentHint false on READ tools, and the four pickup descriptions. Until cutover the
// snapshot file stays as Python wrote it.
func adjusted(t testing.TB, snapshot []map[string]any) []map[string]any {
	t.Helper()
	changed := map[string]bool{}
	for _, tool := range snapshot {
		name := tool["name"].(string)
		ann := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] == true {
			ann["idempotentHint"] = false
		}
		desc := tool["description"].(string)
		switch name {
		case "get_cart":
			tool["description"] = desc + cartAddition
		case "compare_basket":
			const marker = "Only reads: use set_cart_quantities"
			if !strings.Contains(desc, marker) {
				t.Fatal("compare_basket has no 'Only reads' sentence to insert before")
			}
			tool["description"] = strings.Replace(desc, marker, compareAddition+marker, 1)
		case "get_delivery_info":
			tool["description"] = deliveryInfoText
		case "list_delivery_slots":
			tool["description"] = slotsText
		default:
			continue
		}
		changed[name] = true
	}
	if len(changed) != 4 {
		t.Fatalf("%d descriptions adjusted, want 4", len(changed))
	}
	return snapshot
}

func TestToolSurface(t *testing.T) {
	factories, _, _ := standard()
	actual := toolsOnTheWire(t, connect(t, factories))
	if os.Getenv("HANDLE_UPDATE_SNAPSHOT") == "1" {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		if err := enc.Encode(actual); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snapshotPath(), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("snapshot written: check the diff")
	}
	raw, err := os.ReadFile(snapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	want := adjusted(t, snapshot)
	if len(actual) != len(want) {
		t.Fatalf("%d tools, snapshot has %d", len(actual), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(actual[i], want[i]) {
			a, _ := json.MarshalIndent(actual[i], "", " ")
			w, _ := json.MarshalIndent(want[i], "", " ")
			t.Errorf("tool %v differs from the snapshot with the named adjustments\ngot:\n%s\nwant:\n%s", want[i]["name"], a, w)
		}
	}
}

// writes is the allowlist of tools that change the cart (tests/test_tools.py).
var writes = map[string]bool{"set_cart_quantities": true, "add_recipe_to_cart": true}

var allTools = []string{
	"search_products", "search_recipes", "get_recipe", "list_recipes", "list_offers", "compare_basket",
	"get_cart", "set_cart_quantities", "add_recipe_to_cart", "get_delivery_info", "list_delivery_slots",
	"get_orders", "get_order",
}

func TestOnlyAllowedToolsExistAndOnlyCartToolsWrite(t *testing.T) {
	factories, _, _ := standard()
	tools := toolsOnTheWire(t, connect(t, factories))
	var names []string
	for _, tool := range tools {
		name := tool["name"].(string)
		names = append(names, name)
		ann := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] != !writes[name] || ann["destructiveHint"] != writes[name] {
			t.Errorf("%s: annotations %v", name, ann)
		}
		if ann["openWorldHint"] != true {
			t.Errorf("%s: openWorldHint %v", name, ann["openWorldHint"])
		}
	}
	want := append([]string(nil), allTools...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("tools %v, want %v", names, want)
	}
}

func TestWriteToolsAreMarkedAndNeverOrderOrPay(t *testing.T) {
	factories, _, _ := standard()
	found := map[string]string{}
	for _, tool := range toolsOnTheWire(t, connect(t, factories)) {
		ann := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] == false {
			if ann["destructiveHint"] != true {
				t.Errorf("%v is not marked destructive", tool["name"])
			}
			desc := tool["description"].(string)
			if !strings.Contains(desc, "Never orders or pays.") {
				t.Errorf("%v lacks 'Never orders or pays.'", tool["name"])
			}
			found[tool["name"].(string)] = desc
		}
	}
	if len(found) != 2 || found["set_cart_quantities"] == "" || found["add_recipe_to_cart"] == "" {
		t.Fatalf("write tools %v", reflect.ValueOf(found).MapKeys())
	}
	if !strings.Contains(found["add_recipe_to_cart"], "ASK THE HUMAN FIRST") {
		t.Error("add_recipe_to_cart must say ASK THE HUMAN FIRST")
	}
	// ADD adds twice when called twice: not idempotent. WRITE sets absolute counts: idempotent.
	for _, tool := range toolsOnTheWire(t, connect(t, factories)) {
		if ann := tool["annotations"].(map[string]any); writes[tool["name"].(string)] {
			if want := tool["name"] == "set_cart_quantities"; ann["idempotentHint"] != want {
				t.Errorf("%v idempotentHint %v", tool["name"], ann["idempotentHint"])
			}
		}
	}
}

// calls is CALLS of tests/test_privacy.py: every tool once.
var calls = []struct {
	tool string
	args map[string]any
}{
	{"search_products", map[string]any{"query": "mælk", "stores": []string{"fake"}}},
	{"list_offers", map[string]any{"stores": []string{"fake"}}},
	{"search_recipes", map[string]any{"query": "karry", "stores": []string{"fake"}}},
	{"get_recipe", map[string]any{"store": "fake", "recipe": "r1", "persons": 6}},
	{"list_recipes", map[string]any{"stores": []string{"fake"}}},
	{"list_recipes", map[string]any{"stores": []string{"fake"}, "tags": []string{"Familievenlig"}}},
	{"compare_basket", map[string]any{"items": []string{"mælk 1 l", "2 x rugbrød"}, "stores": []string{"fake"}}},
	{"get_cart", map[string]any{"store": "fake"}},
	{"set_cart_quantities", map[string]any{"store": "fake", "items": map[string]int{"1": 1}}},
	{"add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1"}},
	{"get_delivery_info", map[string]any{"store": "fake"}},
	{"list_delivery_slots", map[string]any{"stores": []string{"fake"}}},
	{"get_orders", map[string]any{"store": "fake"}},
	{"get_order", map[string]any{"store": "fake", "order_id": "900000000001"}},
}

func TestEveryToolAnswersJSON(t *testing.T) {
	factories, _, _ := standard()
	s := connect(t, factories)
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.tool] = true
		res := s.call(c.tool, c.args)
		if res.Err != nil || res.IsError {
			t.Errorf("%s: %v %s", c.tool, res.Err, res.Text)
			continue
		}
		decode(t, res.Text)
		for _, secret := range leak {
			if strings.Contains(res.Text, secret) {
				t.Errorf("%s leaked %q", c.tool, secret)
			}
		}
	}
	for _, name := range allTools { // test_every_tool_is_called_above
		if !seen[name] {
			t.Errorf("%s is never called", name)
		}
	}
}

func TestErrorsReachTheModelAsClearText(t *testing.T) {
	factories, _, _ := standard()
	s := connect(t, factories)
	for _, c := range []struct {
		tool string
		args any
		text string
	}{
		{"get_cart", map[string]any{"store": "nope"}, "Ukendt butik 'nope'. Kendte butikker: broken, fake."},
		{"set_cart_quantities", map[string]any{"store": "fake", "items": map[string]int{"1": 99}}, "Antal skal være 0-50. Forkert for: 1."},
		{"set_cart_quantities", map[string]any{"store": "fake", "items": map[string]string{"1": "mange"}}, "Argumentet 'items.1' skal være et heltal."},
		{"add_recipe_to_cart", map[string]any{"store": "fake", "recipe": "r1", "skip": []string{"Pizza"}}, "Opskriften har ikke 'Pizza'. Ingredienser: mælk."},
	} {
		res := s.call(c.tool, c.args)
		if !res.IsError || res.Text != c.text {
			t.Errorf("%s: isError=%v text %q, want %q", c.tool, res.IsError, res.Text, c.text)
		}
	}
}

func TestOneFailingStoreIsAnAnswerNotAToolError(t *testing.T) {
	factories, _, _ := standard()
	res := connect(t, factories).call("search_products", map[string]any{"query": "mælk", "stores": []string{"broken"}})
	want := map[string]any{"broken": map[string]any{"error": "broken: API'et svarer 500"}}
	if res.IsError || !reflect.DeepEqual(decode(t, res.Text), want) {
		t.Errorf("isError=%v answer %s", res.IsError, res.Text)
	}
}

func TestEveryAnswerIsTwoSpaceIndentedJSON(t *testing.T) {
	factories, _, _ := standard()
	res := connect(t, factories).call("get_cart", map[string]any{"store": "fake"})
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(res.Text), "", "  "); err != nil || buf.String() != res.Text {
		t.Errorf("not 2-space indented JSON: %v\n%s", err, res.Text)
	}
}
