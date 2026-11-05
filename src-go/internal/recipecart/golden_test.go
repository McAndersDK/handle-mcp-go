package recipecart_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/goldentest"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/recipecart"
)

const (
	wantPlanCases = 27
	wantErrors    = 8 // plan cases whose golden is a PlanError
)

// orderedPairs reads a JSON object as pairs in file order (Python's dict order).
func orderedPairs(t *testing.T, raw json.RawMessage) []recipecart.Swap {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("swap is not an object: %s", raw)
	}
	var out []recipecart.Swap
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		var v string
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, recipecart.Swap{Key: k.(string), Value: v})
	}
	return out
}

type linesJSON map[string]struct {
	Ingredients []string
	Name        string
	Quantity    int
}

func toMap(lines []recipecart.Line) linesJSON {
	m := linesJSON{}
	for _, l := range lines {
		m[l.ProductID] = struct {
			Ingredients []string
			Name        string
			Quantity    int
		}{l.Ingredients, l.Name, l.Quantity}
	}
	return m
}

func sameLines(got []recipecart.Line, want linesJSON) bool {
	norm := func(m linesJSON) linesJSON {
		for k, v := range m {
			if v.Ingredients == nil {
				v.Ingredients = []string{}
				m[k] = v
			}
		}
		return m
	}
	return reflect.DeepEqual(norm(toMap(got)), norm(want)) && len(got) == len(want)
}

func TestGoldenPlan(t *testing.T) {
	var g struct {
		Cases []struct {
			Name   string
			Recipe model.RecipeDetails
			Args   struct {
				Skip    []string
				Swap    json.RawMessage
				Include []string
			}
			Lines   linesJSON
			Skipped []recipecart.Skipped
			Error   *struct{ Type, Message string }
		}
		InCart struct {
			Cart struct {
				Lines []struct {
					ProductID json.Number `json:"product_id"`
					Quantity  int
				}
			}
			Output map[string]int
		} `json:"in_cart"`
		InCartEmpty struct{ Output map[string]int } `json:"in_cart_empty"`
		InCartNull  struct{ Output map[string]int } `json:"in_cart_lines_null"`
		Report      struct {
			Before  map[string]int
			Results []model.QuantityResult
			Output  []recipecart.ReportLine
		} `json:"report"`
		ReportNone struct {
			Output []recipecart.ReportLine
		} `json:"report_no_results"`
	}
	goldentest.Read(t, "recipecart/plan.json", &g)
	if len(g.Cases) != wantPlanCases {
		t.Fatalf("plan.json has %d cases, want %d", len(g.Cases), wantPlanCases)
	}
	errs := 0
	var kebab model.RecipeDetails
	for _, c := range g.Cases {
		if c.Name == "default" {
			kebab = c.Recipe
		}
		lines, skipped, err := recipecart.Plan(c.Recipe, c.Args.Skip, orderedPairs(t, c.Args.Swap), c.Args.Include)
		if c.Error != nil {
			var pe *recipecart.PlanError
			if !errors.As(err, &pe) || pe.Msg != c.Error.Message || c.Error.Type != "PlanError" {
				t.Errorf("%s: error %v, want PlanError %q", c.Name, err, c.Error.Message)
			}
			errs++
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if !sameLines(lines, c.Lines) {
			t.Errorf("%s: lines %v, want %v", c.Name, toMap(lines), c.Lines)
		}
		if len(skipped) != len(c.Skipped) || (len(skipped) > 0 && !reflect.DeepEqual(skipped, c.Skipped)) {
			t.Errorf("%s: skipped %v, want %v", c.Name, skipped, c.Skipped)
		}
	}
	if errs != wantErrors {
		t.Errorf("%d error cases, want %d", errs, wantErrors)
	}

	// in_cart: the golden's second line has the number 5 for a product id; str(5) is "5"
	cart := model.Cart{}
	for _, l := range g.InCart.Cart.Lines {
		cart.Lines = append(cart.Lines, model.CartLine{ProductID: l.ProductID.String(), Quantity: l.Quantity})
	}
	if got := recipecart.InCart(cart); !reflect.DeepEqual(got, g.InCart.Output) || len(got) != 2 {
		t.Errorf("in_cart: %v, want %v", got, g.InCart.Output)
	}
	if got := recipecart.InCart(model.Cart{}); !reflect.DeepEqual(got, g.InCartEmpty.Output) {
		t.Errorf("in_cart empty: %v", got)
	}
	if got := recipecart.InCart(model.Cart{Lines: nil}); !reflect.DeepEqual(got, g.InCartNull.Output) {
		t.Errorf("in_cart lines null: %v", got)
	}

	// report: lines in the order Plan gives them (the golden's "lines" object is sorted)
	lines, _, err := recipecart.Plan(kebab, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]recipecart.ReportLine{
		"report":            recipecart.Report(lines, g.Report.Before, g.Report.Results),
		"report_no_results": recipecart.Report(lines, map[string]int{}, nil),
	} {
		want := g.Report.Output
		if name != "report" {
			want = g.ReportNone.Output
		}
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		if string(gj) != string(wj) || len(got) != len(lines) {
			t.Errorf("%s: %s\nwant %s", name, gj, wj)
		}
	}
	t.Logf("plan.json: %d plan cases (%d errors) + in_cart x3 + report x2 run, 0 skipped", len(g.Cases), errs)
}
