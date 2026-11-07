// Package goldentest reads the golden files in tests/golden for the Go tests (ADR 0005).
// Only _test.go files import it.
package goldentest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// Path is the absolute path of a file under tests/golden.
func Path(rel string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "tests", "golden", filepath.FromSlash(rel))
}

// Read decodes tests/golden/<rel> into v. Numbers in an `any` become json.Number.
func Read(t testing.TB, rel string, v any) {
	t.Helper()
	raw, err := os.ReadFile(Path(rel))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
}

// Corpus is the products of match/corpus_<name>.json, in file order.
func Corpus(t testing.TB, name string) []model.Product {
	t.Helper()
	var f struct {
		Products []struct {
			Product model.Product `json:"product"`
		} `json:"products"`
	}
	Read(t, "match/corpus_"+name+".json", &f)
	out := make([]model.Product, len(f.Products))
	for i, p := range f.Products {
		out[i] = p.Product
	}
	return out
}

// addedKeys are the answer keys that did not exist when the goldens were recorded (docs/adr/0006).
var addedKeys = []string{"delivery_mode", "pickup_store"}

// DropAdded removes exactly the keys delivery_mode and pickup_store from a decoded answer, at any
// depth, and returns it. Use it on the ACTUAL output before comparing with a golden, so the golden
// still proves every other key and value. A case whose answer changes because of pickup is a named
// exception with a hand-written answer that includes these keys; it must not go through here.
func DropAdded(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range addedKeys {
			delete(x, k)
		}
		for _, e := range x {
			DropAdded(e)
		}
	case []any:
		for _, e := range x {
			DropAdded(e)
		}
	}
	return v
}

// Money parses a golden number; a bad one fails the test.
func Money(t testing.TB, s string) model.Money {
	t.Helper()
	m, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
