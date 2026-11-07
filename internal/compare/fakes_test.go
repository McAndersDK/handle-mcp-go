package compare_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// These are the fakes and lists of upstream/tests/test_compare.py and scripts/golden_compare.py. The
// goldens do not record the catalogs, so they are written out here.

func money(s string) model.Money {
	m, err := model.ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return m
}

func ptr[T any](v T) *T { return &v }

func prod(store, id, name, price string, size string, available bool, brand string) model.Product {
	p := model.NewProduct(store, id, name, money(price))
	if size != "" {
		p.Size = ptr(size)
	}
	if brand != "" {
		p.Brand = ptr(brand)
	}
	p.Available = available
	return p
}

var weekList = []string{
	"2 x letmælk 1 l", "rugbrød", "æg 10 stk", "smør 250 g", "hakket oksekød 500 g", "kyllingebryst",
	"bananer", "æbler", "kartofler 2 kg", "gulerødder 1 kg", "pasta 500 g", "havregryn 1 kg",
	"kaffe 400 g", "toiletpapir", "skyr 1 kg",
}

var nemligCatalog = []model.Product{
	prod("nemlig", "n1", "Letmælk 0,5% fedt", "11.95", "1 l", true, ""),
	prod("nemlig", "n2", "Kakaomælk", "9.95", "1 l", true, ""),
	prod("nemlig", "n3", "Rugbrød m. kerner", "22.00", "1 kg", true, "Schulstad"),
	prod("nemlig", "n4", "Økologiske æg", "34.95", "10 stk", true, ""),
	prod("nemlig", "n5", "Smør", "24.95", "250 g", true, "Lurpak"),
	prod("nemlig", "n6", "Hakket oksekød 8-12%", "45.00", "500 g", true, ""),
	prod("nemlig", "n7", "Kyllingebryst", "59.00", "900 g", true, ""),
	prod("nemlig", "n8", "Bananer", "3.00", "1 stk", true, ""),
	prod("nemlig", "n9", "Æbler Pink Lady", "25.00", "1 kg", true, ""),
	prod("nemlig", "n10", "Kartofler", "20.00", "2 kg", true, ""),
	prod("nemlig", "n11", "Gulerødder", "12.00", "1 kg", true, ""),
	prod("nemlig", "n12", "Pasta penne", "14.00", "500 g", true, ""),
	prod("nemlig", "n13", "Havregryn", "16.00", "1 kg", true, ""),
	prod("nemlig", "n14", "Kaffe formalet", "45.00", "500 g", true, ""), // only 500 g
	prod("nemlig", "n15", "Toiletpapir 8 ruller", "39.00", "8 stk", true, ""),
	prod("nemlig", "n16", "Skyr vanilje", "30.00", "1 kg", true, ""),
}

var bilkaCatalog = []model.Product{
	prod("bilkatogo", "b1", "Letmælk", "10.50", "1 l", true, ""),
	prod("bilkatogo", "b2", "Letmælk", "21.00", "2 l", true, ""),
	prod("bilkatogo", "b3", "Rugbrød", "18.00", "1 kg", true, ""),
	prod("bilkatogo", "b4", "Æg fra fritgående høns", "29.00", "10 stk", true, ""),
	prod("bilkatogo", "b5", "Smør", "22.00", "250 g", true, ""),
	prod("bilkatogo", "b6", "Hakket oksekød 8-12%", "40.00", "500 g", true, ""),
	prod("bilkatogo", "b7", "Kyllingebrystfilet", "55.00", "900 g", true, ""), // word inside a word
	prod("bilkatogo", "b8", "Bananer", "2.50", "1 stk", true, ""),
	prod("bilkatogo", "b9", "Æbler", "22.00", "1 kg", true, ""),
	prod("bilkatogo", "b10", "Kartofler", "18.00", "2 kg", true, ""),
	prod("bilkatogo", "b11", "Gulerødder", "10.00", "1 kg", true, ""),
	prod("bilkatogo", "b12", "Pasta fusilli", "12.00", "500 g", true, ""),
	prod("bilkatogo", "b13", "Havregryn", "15.00", "1 kg", true, ""),
	prod("bilkatogo", "b14", "Kaffe", "42.00", "400 g", true, ""),
	prod("bilkatogo", "b15", "Toiletpapir", "35.00", "8 stk", false, ""), // sold out
	prod("bilkatogo", "b16", "Skyr naturel", "28.00", "1 kg", true, ""),
}

func terms(storeName string, home bool, fee string, other map[string]string, minimum string) model.StoreTerms {
	t := model.NewStoreTerms(storeName, home)
	if fee != "" {
		t.DeliveryFee = ptr(money(fee))
	}
	if other != nil {
		t.OtherFees = model.Dict[model.Money]{}
		for k, v := range other {
			t.OtherFees[k] = money(v)
		}
	}
	if minimum != "" {
		t.MinimumOrder = ptr(money(minimum))
	}
	return t
}

var (
	termsN = terms("nemlig", true, "29", map[string]string{"poser": "6"}, "300")
	termsB = terms("bilkatogo", true, "39", map[string]string{"pakkeservice": "39"}, "500")
	pickup = terms("bilkatogo", false, "", nil, "")
)

// fake is CatalogStore / Scenario / Broken of the Python tests. Methods it does not have panic
// (the embedded interface is nil): a test that reaches one has a bug.
type fake struct {
	store.Store
	name     string
	catalog  []model.Product
	terms    model.StoreTerms
	info     model.DeliveryInfo
	infoErr  error
	searchEr error
	searches []string
}

func (f *fake) Name() string { return f.name }

func (f *fake) Search(_ context.Context, query string, limit int) ([]model.Product, error) {
	f.searches = append(f.searches, query)
	if f.searchEr != nil {
		return nil, f.searchEr
	}
	_, words, _ := match.ParseItem(query)
	var found []model.Product
	for _, x := range f.catalog {
		name := pystr.Lower(x.Name)
		for _, w := range words {
			if r := []rune(w); len(r) > 1 && strings.Contains(name, string(r[:min(5, len(r))])) {
				found = append(found, x) // a copy: the caller may sort it
				break
			}
		}
	}
	return found[:min(limit, len(found))], nil
}

func (f *fake) Terms(context.Context) (model.StoreTerms, error) { return f.terms, nil }

func (f *fake) DeliveryInfo(context.Context) (model.DeliveryInfo, error) {
	return f.info, f.infoErr
}

// out is the one exit of an answer, decoded again: JSON with json.Number, like the golden.
func out(t testing.TB, v any) any {
	t.Helper()
	text, err := privacy.Out(v)
	if err != nil {
		t.Fatal(err)
	}
	return decode(t, []byte(text))
}

func decode(t testing.TB, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// diff names the first place two decoded JSON values differ, with short values: a golden is large.
func diff(path string, got, want any) string {
	if reflect.DeepEqual(got, want) {
		return ""
	}
	switch g := got.(type) {
	case map[string]any:
		w, ok := want.(map[string]any)
		if !ok {
			break
		}
		for k := range g {
			if _, there := w[k]; !there {
				return fmt.Sprintf("%s: extra key %q", path, k)
			}
		}
		for k, wv := range w {
			gv, there := g[k]
			if !there {
				return fmt.Sprintf("%s: missing key %q", path, k)
			}
			if d := diff(path+"."+k, gv, wv); d != "" {
				return d
			}
		}
	case []any:
		w, ok := want.([]any)
		if !ok {
			break
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s: %d elements, want %d", path, len(g), len(w))
		}
		for i := range g {
			if d := diff(fmt.Sprintf("%s[%d]", path, i), g[i], w[i]); d != "" {
				return d
			}
		}
	}
	return fmt.Sprintf("%s: got %s, want %s", path, short(got), short(want))
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

var cph = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		panic(err)
	}
	return loc
}()
