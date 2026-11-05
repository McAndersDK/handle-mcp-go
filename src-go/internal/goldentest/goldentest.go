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

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
)

// Path is the absolute path of a file under tests/golden.
func Path(rel string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "tests", "golden", filepath.FromSlash(rel))
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

// Money parses a golden number; a bad one fails the test.
func Money(t testing.TB, s string) model.Money {
	t.Helper()
	m, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
