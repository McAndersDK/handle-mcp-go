package model_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
)

const goldenDir = "../../../tests/golden/privacy"

// Expected case counts. A golden that shrinks, or a case the test does not read, fails.
const (
	wantDecimals  = 34
	wantDatetimes = 7
	wantSkipped   = 4 // NaN, sNaN, Infinity, -Infinity: ADR 0005
)

func TestGoldenMoney(t *testing.T) {
	var g struct {
		Decimals  []struct{ Input, Output string }
		Datetimes []struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		}
		Nested map[string]json.RawMessage `json:"nested"`
	}
	raw, err := os.ReadFile(filepath.Join(goldenDir, "money.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Decimals) != wantDecimals || len(g.Datetimes) != wantDatetimes {
		t.Fatalf("money.json has %d decimals and %d datetimes, want %d and %d",
			len(g.Decimals), len(g.Datetimes), wantDecimals, wantDatetimes)
	}
	run, skipped := 0, 0
	for _, c := range g.Decimals {
		// SKIPPED (ADR 0005): NaN and Infinity cannot be a shopspring decimal. Parsing
		// must fail so the store adapter can raise a StoreError.
		switch c.Input {
		case "NaN", "sNaN", "Infinity", "-Infinity":
			if _, err := model.ParseMoney(c.Input); err == nil {
				t.Errorf("ParseMoney(%q) must fail", c.Input)
			}
			skipped++
			continue
		}
		m, err := model.ParseMoney(c.Input)
		if err != nil {
			t.Errorf("ParseMoney(%q): %v", c.Input, err)
			continue
		}
		if got := m.String(); got != c.Output {
			t.Errorf("Money(%q) = %q, want %q", c.Input, got, c.Output)
		}
		// the same through JSON, as a string and as a raw number (never float64)
		for _, in := range []string{`"` + c.Input + `"`, c.Input} {
			var j model.Money
			if err := json.Unmarshal([]byte(in), &j); err != nil {
				continue // "1E+2" and the like are valid JSON numbers only in some spellings
			}
			if j.String() != c.Output {
				t.Errorf("json %s = %q, want %q", in, j.String(), c.Output)
			}
		}
		run++
	}
	if skipped != wantSkipped || run+skipped != wantDecimals {
		t.Fatalf("decimals: %d run, %d skipped, want %d skipped of %d", run, skipped, wantSkipped, wantDecimals)
	}
	for _, c := range g.Datetimes {
		var out string
		if len(c.Input) == 10 {
			d, err := model.ParseDate(c.Input)
			if err != nil {
				t.Errorf("ParseDate(%q): %v", c.Input, err)
			}
			out = d.String()
		} else {
			v, err := model.ParseDateTime(c.Input)
			if err != nil {
				t.Errorf("ParseDateTime(%q): %v", c.Input, err)
			}
			out = v.String()
			if b, _ := json.Marshal(v); string(b) != `"`+c.Output+`"` {
				t.Errorf("json of %q = %s", c.Input, b)
			}
		}
		if out != c.Output {
			t.Errorf("%q -> %q, want %q", c.Input, out, c.Output)
		}
	}
	t.Logf("money.json: %d decimals run, %d skipped (NaN/Infinity), %d datetimes run, nested + recipe checked",
		run, skipped, len(g.Datetimes))
}

// TestGoldenNestedAndRecipe builds the golden's "nested" and "recipe_text_is_unchanged"
// answers from Go values and compares them as parsed JSON.
func TestGoldenNestedAndRecipe(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(goldenDir, "money.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nested struct{ Output json.RawMessage } `json:"nested"`
		Recipe struct{ Output json.RawMessage } `json:"recipe_text_is_unchanged"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	day, _ := model.ParseDate("2026-11-02")
	nested := map[string]any{
		"fees":  map[string]any{"levering": model.NewMoney(39, 0), "poser": model.NewMoney(0, 0)},
		"flags": []any{true, false, nil, json.Number("3"), json.Number("1.5"), json.Number("249.87")},
		"lines": []any{
			map[string]any{"unit_price": mustMoney(t, "0.125"), "when": nil},
			map[string]any{"unit_price": mustMoney(t, "12.995"), "when": day},
		},
		"tuple": []any{mustMoney(t, "1"), "x", nil},
	}
	sameJSON(t, nested, g.Nested.Output)

	title, name := "Kebabfad", "Kebabfad"
	desc, image, tip := "Prøv denne lækre opskrift", "https://media.example.com/a.jpg", "Server med citron"
	persons := 4
	recipe := model.RecipeDetails{
		Store: "nemlig", ID: "kebab", Name: name, URL: "https://example.com/x", Persons: &persons,
		Description: &desc, Image: &image, Tip: &tip,
		Method: model.List[model.RecipeSection]{{Title: &title, Steps: model.List[string]{"Tænd ovnen på 200 grader"}}},
	}
	sameJSON(t, recipe, g.Recipe.Output)
}

func mustMoney(t *testing.T, s string) model.Money {
	t.Helper()
	m, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sameJSON(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var a, w any
	for _, p := range []struct {
		raw []byte
		to  *any
	}{{b, &a}, {want, &w}} {
		dec := json.NewDecoder(bytes.NewReader(p.raw))
		dec.UseNumber()
		if err := dec.Decode(p.to); err != nil {
			t.Fatal(err)
		}
	}
	if canon(a) != canon(w) {
		t.Errorf("got  %s\nwant %s", canon(a), canon(w))
	}
}

// canon prints decoded JSON with sorted keys, so two values compare as text.
func canon(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
