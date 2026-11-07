package server

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/internal/candidates"
	"github.com/McAndersDK/handle-mcp-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
	"github.com/McAndersDK/handle-mcp-go/internal/recipecart"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// leakMap is LEAK of upstream/tests/conftest.py: personal data a sloppy adapter could let through.
var leakMap = map[string]any{
	"CustomerName":    "Anders And",
	"Email":           "anders@example.com",
	"MobileNumber":    "+45 98 76 54 32",
	"DoorCode":        "kode-4711",
	"UnattendedNotes": "Nøglen ligger under måtten",
	"deliveryAddress": map[string]any{"street": "Andeby 1", "zip": "postnr-9999", "city": "Andeby"},
}

// leak is every value of leakMap, nested ones too.
var leak = []string{
	"Anders And", "anders@example.com", "+45 98 76 54 32", "kode-4711", "Nøglen ligger under måtten",
	"Andeby 1", "postnr-9999", "Andeby",
}

// handlerFor runs fn through the same wrapper every tool uses and returns its text.
func handlerFor(t *testing.T, fn toolFunc) (text string, isError bool) {
	t.Helper()
	factories, _, _ := standard()
	h := New(factories, "test").wrap(toolDef{Name: "leaky"}, fn)
	res, err := h(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "leaky"}})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// ADR 0005 test 1: a handler that returns leaky data comes out clean, because the exit scrubs.
func TestExitScrubsWhateverAHandlerReturns(t *testing.T) {
	type leakyStruct struct { // a typed answer with personal fields, as a sloppy adapter might add
		Store           string         `json:"store"`
		CustomerName    string         `json:"CustomerName"`
		DeliveryAddress map[string]any `json:"deliveryAddress"`
	}
	answers := map[string]any{
		"map":    mergeLeak(map[string]any{"store": "fake"}),
		"nested": map[string]any{"fake": []any{mergeLeak(map[string]any{"id": "1"})}},
		"struct": []leakyStruct{{"fake", "Anders And", leakMap["deliveryAddress"].(map[string]any)}},
	}
	for name, value := range answers {
		text, isError := handlerFor(t, func(context.Context, Args) (any, error) { return value, nil })
		if isError {
			t.Fatalf("%s: isError", name)
		}
		for _, secret := range leak {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaked %q:\n%s", name, secret, text)
			}
		}
		if !strings.Contains(text, privacy.Redacted) {
			t.Errorf("%s: nothing was redacted:\n%s", name, text)
		}
	}
}

func mergeLeak(m map[string]any) map[string]any {
	for k, v := range leakMap {
		m[k] = v
	}
	return m
}

// ADR 0005 test 3: free text and errors are scrubbed by the exit.
func TestExitScrubsFreeTextAndErrors(t *testing.T) {
	text, _ := handlerFor(t, func(context.Context, Args) (any, error) {
		return map[string]any{
			"name":     "Mælk 1 l, Kildevej Mejeri", // a product name passes as the store wrote it
			"warnings": []any{"Ring til os på 98765432", "Adresse: Kildevej 12, 2100 København"},
			"results":  []any{map[string]any{"message": "Sendt til anders@example.com"}},
			"notes":    []any{"Levering til Søndergaden 5"},
			"extra":    map[string]any{"error": "Kunden 98765432 findes ikke"},
		}, nil
	})
	for _, word := range []string{"98765432", "Kildevej 12", "2100", "anders@", "Søndergaden 5"} {
		if strings.Contains(text, word) {
			t.Errorf("%q survived:\n%s", word, text)
		}
	}
	if !strings.Contains(text, "Mælk 1 l, Kildevej Mejeri") {
		t.Errorf("the product name was changed:\n%s", text)
	}
	for _, e := range []error{
		toolErrorf("Ordren til Kildevej 12, 2100 København er sendt til a@b.dk"),
		errors.New("anders@example.com"),
	} {
		text, isError := handlerFor(t, func(context.Context, Args) (any, error) { return nil, e })
		if !isError || strings.Contains(text, "Kildevej") || strings.Contains(text, "a@b.dk") || strings.Contains(text, "anders@") {
			t.Errorf("error text %q (isError %v)", text, isError)
		}
	}
}

func TestStoreErrorTextIsScrubbed(t *testing.T) { // test_store_error_text_is_scrubbed
	factories, _, broken := standard()
	broken.ordersFn = func(int) ([]model.Order, error) {
		return nil, store.Errorf("Ordren til Kildevej 12, 2100 København er sendt til a@b.dk")
	}
	res := connect(t, factories).call("get_orders", map[string]any{"store": "broken"})
	if !res.IsError || strings.Contains(res.Text, "Kildevej") || strings.Contains(res.Text, "a@b.dk") || !strings.Contains(res.Text, "broken:") {
		t.Errorf("isError %v text %q", res.IsError, res.Text)
	}
}

func TestStoreErrorInAManyStoreAnswerIsScrubbed(t *testing.T) {
	factories, _, broken := standard()
	broken.searchFn = func(string, int) ([]model.Product, error) { return nil, store.Errorf("Kunden 98765432 findes ikke") }
	res := connect(t, factories).call("search_products", map[string]any{"query": "mælk", "stores": []string{"broken"}})
	if res.IsError || strings.Contains(res.Text, "98765432") {
		t.Errorf("isError %v text %q", res.IsError, res.Text)
	}
}

func TestUnexpectedErrorShowsOnlyTheType(t *testing.T) {
	factories, _, broken := standard()
	broken.searchFn = func(string, int) ([]model.Product, error) {
		return nil, errors.New("GET https://example.com/api?token=secret-token failed")
	}
	res := connect(t, factories).call("search_products", map[string]any{"query": "mælk", "stores": []string{"broken"}})
	if strings.Contains(res.Text, "secret-token") || !strings.Contains(res.Text, "uventet fejl (*errors.errorString)") {
		t.Errorf("text %q", res.Text)
	}
	broken.searchFn = func(string, int) ([]model.Product, error) { panic("crash with token secret-token") }
	res = connect(t, factories).call("search_products", map[string]any{"query": "mælk", "stores": []string{"broken"}})
	if res.IsError || strings.Contains(res.Text, "secret-token") || !strings.Contains(res.Text, "uventet fejl (string)") {
		t.Errorf("a panic in a store must be an answer: isError %v text %q", res.IsError, res.Text)
	}
}

// ADR 0005 test 4: every key of every answer type is passthrough, free text or personal, and none is personal.
func TestEveryAnswerKeyIsClassified(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeFor[model.Product](), reflect.TypeFor[model.Recipe](), reflect.TypeFor[model.RecipeDetails](),
		reflect.TypeFor[model.Cart](), reflect.TypeFor[model.QuantityResult](), reflect.TypeFor[model.StoreTerms](),
		reflect.TypeFor[model.DeliveryInfo](), reflect.TypeFor[model.DeliverySlot](), reflect.TypeFor[model.Order](),
		// what the server adds
		reflect.TypeFor[compareAnswer](), reflect.TypeFor[setAnswer](), reflect.TypeFor[addAnswer](),
		reflect.TypeFor[deliveryAnswer](), reflect.TypeFor[slotsAnswer](), reflect.TypeFor[ordersAnswer](),
		reflect.TypeFor[errorAnswer](), reflect.TypeFor[compare.BasketQuote](), reflect.TypeFor[compare.BasketLine](),
		reflect.TypeFor[compare.VerdictAnswer](), reflect.TypeFor[candidates.Candidate](),
		reflect.TypeFor[recipecart.ReportLine](), reflect.TypeFor[recipecart.Skipped](),
	}
	if keys := privacy.UnclassifiedKeys(types...); len(keys) > 0 {
		t.Errorf("unclassified answer keys: %v (add to privacy.PassthroughKeys if harmless)", keys)
	}
	if keys := privacy.PersonalKeysOf(types...); len(keys) > 0 {
		t.Errorf("personal keys in an answer: %v", keys)
	}
	var free []string
	for _, k := range privacy.JSONKeys(types...) {
		if privacy.Classify(k) == privacy.FreeText {
			free = append(free, k)
		}
	}
	if want := []string{"error", "message", "note", "notes", "warnings"}; !reflect.DeepEqual(free, want) {
		t.Errorf("free-text keys %v, want %v", free, want)
	}
	for _, k := range privacy.JSONKeys(types...) { // the embedded struct is flattened into its parent
		if k == "VerdictAnswer" {
			t.Errorf("the embedded struct is a key of its own")
		}
	}
	if n := len(privacy.JSONKeys(types...)); n < 90 {
		t.Errorf("only %d keys found: the walker misses fields", n)
	}
}

// CallToolResult and TextContent are built in out.go only: the one exit (CLAUDE.md rule 2).
func TestResultsAreBuiltOnlyInTheExit(t *testing.T) {
	checked := 0
	visit := func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		if filepath.Base(path) == "out.go" && filepath.Base(filepath.Dir(path)) == "server" {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			var typ ast.Expr
			switch x := n.(type) {
			case *ast.CompositeLit:
				typ = x.Type
			case *ast.CallExpr: // new(mcp.CallToolResult)
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
					typ = x.Args[0]
				}
			case *ast.ValueSpec: // var r mcp.CallToolResult
				typ = x.Type
			}
			if sel, ok := typ.(*ast.SelectorExpr); ok && (sel.Sel.Name == "CallToolResult" || sel.Sel.Name == "TextContent") {
				t.Errorf("%s builds a %s outside the exit (server/out.go)", path, sel.Sel.Name)
			}
			return true
		})
		return nil
	}
	err := filepath.WalkDir(filepath.Join("..", "..", "internal"), visit)
	if err == nil {
		err = filepath.WalkDir(filepath.Join("..", "..", "cmd"), visit)
	}
	if err != nil || checked < 50 {
		t.Fatalf("checked %d files: %v", checked, err)
	}
}

func TestTheCheckFindsAResultBuiltElsewhere(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\nvar _ = &mcp.CallToolResult{}\nvar _ = mcp.TextContent{}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok {
			if sel, ok := lit.Type.(*ast.SelectorExpr); ok && (sel.Sel.Name == "CallToolResult" || sel.Sel.Name == "TextContent") {
				found++
			}
		}
		return true
	})
	if found != 2 {
		t.Fatalf("found %d, want 2", found)
	}
}
