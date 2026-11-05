package compare_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/goldentest"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/privacy"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// Expected case counts. A golden that shrinks, or a case the test does not read, fails.
const (
	wantBestAll   = 171 * 3 // items x days, per corpus
	wantQuote     = 57
	wantVerdict   = 42
	wantToolError = 12 // server.compare_basket's ToolError for an empty or a too long list
	wantSearch    = 77
)

var wantPerSearch = map[string]int{"nemlig": 390, "bilkatogo": 468, "synthetic": 0}

func dayOf(t *testing.T, key string) *model.Date {
	t.Helper()
	if key == "null" {
		return nil
	}
	d, err := model.ParseDate(key)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestGoldenBestMatch(t *testing.T) {
	for name, wantSearchRuns := range wantPerSearch {
		products := goldentest.Corpus(t, name)
		var g struct {
			Days      []string
			All       map[string]map[string]json.RawMessage
			PerSearch map[string]struct {
				Indexes []int
				Results map[string]map[string]json.RawMessage
			} `json:"per_search"`
		}
		goldentest.Read(t, "compare/best_match_"+name+".json", &g)
		if !slices.Equal(g.Days, []string{"null", "2026-11-02", "2030-01-01"}) {
			t.Fatalf("%s: days %v", name, g.Days)
		}
		run := func(label, item string, indexes []int, day, raw string) {
			sub := make([]model.Product, len(indexes))
			for i, ix := range indexes {
				sub[i] = products[ix]
			}
			line := compare.BestMatch(item, sub, dayOf(t, day))
			got := out(t, line).(map[string]any)
			got["product_index"] = nil
			for k := range sub {
				if line.Product == &sub[k] {
					got["product_index"] = json.Number(strconv.Itoa(indexes[k]))
				}
			}
			if d := diff("", got, decode(t, []byte(raw))); d != "" {
				t.Errorf("%s: %q on %s: %s", label, item, day, d)
			}
		}
		all := make([]int, len(products))
		for i := range all {
			all[i] = i
		}
		n := 0
		for item, days := range g.All {
			if len(days) != 3 {
				t.Fatalf("%s: %q has %d days", name, item, len(days))
			}
			for day, raw := range days {
				run(name+"/all", item, all, day, string(raw))
				n++
			}
		}
		if n != wantBestAll {
			t.Errorf("%s: %d best_match runs, want %d", name, n, wantBestAll)
		}
		m := 0
		for group, ps := range g.PerSearch {
			for item, days := range ps.Results {
				for day, raw := range days {
					run(name+"/"+group, item, ps.Indexes, day, string(raw))
					m++
				}
			}
		}
		if m != wantSearchRuns {
			t.Errorf("%s: %d per-search runs, want %d", name, m, wantSearchRuns)
		}
		t.Logf("best_match_%s.json: %d + %d runs, 0 skipped", name, n, m)
	}
}

func chosen(naive bool, t time.Time) *model.DateTime {
	v := model.NewDateTime(t)
	if naive {
		v = model.NaiveDateTime(t)
	}
	return &v
}

func date(t *testing.T, s string) *model.Date {
	t.Helper()
	d, err := model.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestGoldenQuote(t *testing.T) {
	type delivery struct {
		name string
		info model.DeliveryInfo
	}
	deliveries := func() []delivery {
		return []delivery{
			{"none", model.DeliveryInfo{}},
			{"chosen_naive", model.DeliveryInfo{ChosenStart: chosen(true, time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC))}},
			{"chosen_utc_after_midnight_in_dk", model.DeliveryInfo{ChosenStart: chosen(false, time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC))}},
			{"chosen_dk_summer", model.DeliveryInfo{ChosenStart: chosen(false, time.Date(2026, 7, 1, 0, 30, 0, 0, cph))}},
			{"chosen_plus9", model.DeliveryInfo{ChosenStart: chosen(false, time.Date(2026, 11, 4, 20, 0, 0, 0, time.FixedZone("", 9*3600)))}},
			{"next_available", model.DeliveryInfo{NextAvailable: date(t, "2026-11-09")}},
			{"note", model.DeliveryInfo{Note: ptr("Gebyret afhænger af tiden (19-51 kr.).")}},
			{"chosen_and_next", model.DeliveryInfo{ChosenStart: chosen(true, time.Date(2026, 11, 3, 16, 0, 0, 0, time.UTC)), NextAvailable: date(t, "2030-01-01")}},
		}
	}
	short := map[string]bool{"none": true, "chosen_utc_after_midnight_in_dk": true, "next_available": true, "note": true}
	lists := []struct {
		name  string
		items []string
	}{
		{"week", weekList},
		{"rugbroed", []string{"rugbrød"}},
		{"missing", []string{"findes ikke xyz"}},
		{"milk_and_missing", []string{"2 x letmælk 1 l", "findes ikke xyz", "toiletpapir"}},
		{"sizes", []string{"kaffe 400 g", "hakket oksekød 1,5 kg", "6 x bananer", "pasta"}},
	}
	type scenario struct {
		name  string
		items []string
		s     *fake
	}
	var cases []scenario
	for _, st := range []struct {
		name    string
		catalog []model.Product
		terms   model.StoreTerms
	}{{"nemlig", nemligCatalog, termsN}, {"bilkatogo", bilkaCatalog, termsB}} {
		for _, l := range lists {
			for _, d := range deliveries() {
				if !short[d.name] && l.name != "sizes" {
					continue
				}
				cases = append(cases, scenario{st.name + "/" + l.name + "/" + d.name, l.items,
					&fake{name: "catalog", catalog: st.catalog, terms: st.terms, info: d.info}})
			}
		}
	}
	one := []string{"rugbrød"}
	cases = append(cases,
		scenario{"delivery_info_not_implemented", one, &fake{catalog: nemligCatalog, terms: termsN, infoErr: store.ErrNotImplemented}},
		scenario{"delivery_info_store_error", one, &fake{catalog: nemligCatalog, terms: termsN, infoErr: store.Errorf("ingen leveringsinfo")}},
		scenario{"pickup_with_note", one, &fake{catalog: bilkaCatalog, terms: pickup, info: model.DeliveryInfo{Note: ptr("Kurven er sat til afhentning.")}}},
		scenario{"pickup_no_note", one, &fake{catalog: bilkaCatalog, terms: pickup}},
	)
	for _, m := range []struct{ label, minimum string }{
		{"exactly_met", "18.00"}, {"none", ""}, {"fraction", "100.005"}, {"huge", "1E+3"}, {"zero", "0"},
	} {
		tm := terms("bilkatogo", true, "39.5", map[string]string{"x": "0", "y": "0.1"}, m.minimum)
		cases = append(cases, scenario{"terms_minimum_" + m.label, one, &fake{catalog: bilkaCatalog, terms: tm}})
	}

	var g struct {
		Cases []struct {
			Name     string
			Items    []string
			Searches []string
			Quote    json.RawMessage
		}
	}
	goldentest.Read(t, "compare/quote.json", &g)
	if len(g.Cases) != wantQuote || len(cases) != wantQuote {
		t.Fatalf("quote.json has %d cases and the test builds %d, want %d", len(g.Cases), len(cases), wantQuote)
	}
	for i, c := range cases {
		want := g.Cases[i]
		if c.name != want.Name || !slices.Equal(c.items, want.Items) {
			t.Fatalf("case %d is %q in the test and %q in the golden", i, c.name, want.Name)
		}
		q, err := compare.Quote(context.Background(), c.s, c.items)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !slices.Equal(c.s.searches, want.Searches) {
			t.Errorf("%s: searches %q, want %q", c.name, c.s.searches, want.Searches)
		}
		if d := diff("", out(t, q), decode(t, want.Quote)); d != "" {
			t.Errorf("%s: %s", c.name, d)
		}
	}
	t.Logf("quote.json: %d cases run, 0 skipped", len(cases))
}

// errorText is server._error_text for a StoreError: "<store>: <message>", scrubbed.
func errorText(name string, err error) string {
	var se *store.StoreError
	if !errors.As(err, &se) {
		return name + ": uventet fejl"
	}
	return privacy.ScrubText(name + ": " + err.Error()).(string)
}

func TestGoldenVerdict(t *testing.T) {
	nemlig := func() store.Store { return &fake{name: "nemlig", catalog: nemligCatalog, terms: termsN} }
	bilka := func() store.Store { return &fake{name: "bilkatogo", catalog: bilkaCatalog, terms: termsB} }
	pickupHome := func() store.Store { return &fake{name: "bilkatogo", catalog: bilkaCatalog, terms: pickup} }
	broken := func() store.Store {
		return &fake{name: "catalog", catalog: bilkaCatalog, terms: termsB, searchEr: store.Errorf("API'et svarer 500")}
	}
	type setup map[string]func() store.Store
	setups := []struct {
		name string
		s    setup
	}{
		{"both", setup{"nemlig": nemlig, "bilkatogo": bilka}},
		{"one_ok_one_broken", setup{"nemlig": nemlig, "broken": broken}},
		{"two_ok_one_broken", setup{"nemlig": nemlig, "bilkatogo": bilka, "broken": broken}},
		{"all_broken", setup{"a": broken, "b": broken}},
		{"single", setup{"nemlig": nemlig}},
		{"pickup_and_home", setup{"nemlig": nemlig, "bilkatogo": pickupHome}},
	}
	tooLong := make([]string, 51)
	for i := range tooLong {
		tooLong[i] = "mælk"
	}
	lists := []struct {
		name  string
		items []string
	}{
		{"week", weekList}, {"rugbroed", []string{"rugbrød"}}, {"none_common", []string{"findes ikke xyz"}},
		{"toilet_and_pasta", []string{"toiletpapir", "pasta 500 g"}},
		{"padded", []string{" mælk 1 l ", "", "2 x rugbrød"}}, {"empty", []string{" ", ""}}, {"too_long", tooLong},
	}
	var g struct {
		Cases []struct {
			Name      string
			Items     []string
			Answer    json.RawMessage
			ToolError string `json:"tool_error"`
		}
	}
	goldentest.Read(t, "compare/verdict.json", &g)
	if len(g.Cases) != wantVerdict {
		t.Fatalf("verdict.json has %d cases, want %d", len(g.Cases), wantVerdict)
	}
	i, run, skipped := 0, 0, 0
	for _, su := range setups {
		for _, l := range lists {
			want := g.Cases[i]
			i++
			if want.Name != su.name+"/"+l.name {
				t.Fatalf("case %d is %q in the golden, the test expects %s/%s", i-1, want.Name, su.name, l.name)
			}
			var items []string // server.compare_basket: strip, drop empty ones
			for _, it := range l.items {
				if s := pystr.Strip(it); s != "" {
					items = append(items, s)
				}
			}
			if len(items) == 0 || len(items) > 50 {
				// SKIPPED: a ToolError of server.compare_basket (phase 7), never reaches compare.
				if want.ToolError == "" {
					t.Errorf("%s: golden has an answer for a list the server refuses", want.Name)
				}
				skipped++
				continue
			}
			names := make([]string, 0, len(su.s))
			for n := range su.s {
				names = append(names, n)
			}
			sort.Strings(names) // server._names: sorted(registry.STORES)
			targets := make([]compare.Target, len(names))
			for k, n := range names {
				targets[k] = compare.Target{Name: n, Store: su.s[n]()}
			}
			results := compare.Gather(context.Background(), targets, func(ctx context.Context, s store.Store) (compare.BasketQuote, error) {
				return compare.Quote(ctx, s, items)
			})
			stores := map[string]any{}
			for _, r := range results {
				if r.Err != nil {
					stores[r.Name] = map[string]string{"error": errorText(r.Name, r.Err)}
				} else {
					stores[r.Name] = r.Value
				}
			}
			answer := struct {
				Stores map[string]any `json:"stores"`
				compare.VerdictAnswer
			}{stores, compare.Verdict(results)}
			if d := diff("", out(t, answer), decode(t, want.Answer)); d != "" {
				t.Errorf("%s: %s", want.Name, d)
			}
			run++
		}
	}
	if skipped != wantToolError || run+skipped != wantVerdict {
		t.Errorf("%d run, %d skipped, want %d skipped of %d", run, skipped, wantToolError, wantVerdict)
	}
	t.Logf("verdict.json: %d cases run, %d skipped (ToolError of the server, phase 7)", run, skipped)
}

func TestGoldenSearchSort(t *testing.T) {
	p := func(id, price, unit string) model.Product {
		x := model.NewProduct("x", id, id, money(price))
		if unit != "" {
			x.UnitPrice = ptr(money(unit))
		}
		return x
	}
	sets := []struct {
		name     string
		products []model.Product
	}{
		{"mixed", []model.Product{p("a", "30", ""), p("b", "20", "80"), p("c", "10", "90")}},
		{"ties", []model.Product{p("t1", "10", "5"), p("t2", "10", "5"), p("t3", "9.99", "5.00"), p("t4", "10.00", "4")}},
		{"gift_first", []model.Product{p("bread", "25", "25"), p("gift", "0", "0")}},
		{"all_unknown", []model.Product{p("u1", "5", ""), p("u2", "4", "")}},
		{"decimals", []model.Product{p("d1", "0.125", "0.0001"), p("d2", "0.12", "0.00010"), p("d3", "1e1", "1E+1")}},
		{"empty", nil},
	}
	var g struct {
		Cases []struct {
			Set    string
			Sort   string
			Limit  int
			Calls  [][]any
			IDs    []string `json:"ids"`
			Answer json.RawMessage
		}
	}
	goldentest.Read(t, "compare/search_sort.json", &g)
	if len(g.Cases) != wantSearch {
		t.Fatalf("search_sort.json has %d cases, want %d", len(g.Cases), wantSearch)
	}
	i := 0
	for _, set := range sets {
		type run struct {
			sort  string
			limit int
		}
		var runs []run
		for _, s := range []string{"relevance", "price", "unit_price"} {
			for _, l := range []int{1, 2, 3, 10} {
				runs = append(runs, run{s, l})
			}
		}
		if set.name == "mixed" {
			for _, l := range []int{-5, 0, 25, 26, 1000} {
				runs = append(runs, run{"price", l})
			}
		}
		for _, r := range runs {
			want := g.Cases[i]
			i++
			if want.Set != set.name || want.Sort != r.sort || want.Limit != r.limit {
				t.Fatalf("case %d is %s/%s/%d in the golden", i-1, want.Set, want.Sort, want.Limit)
			}
			limit := max(1, min(r.limit, 25)) // server.search_products clamps the limit
			fs := &limitStore{products: set.products}
			hits, err := compare.Search(context.Background(), fs, "kylling", limit, r.sort)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fs.calls, want.Calls) {
				t.Errorf("%s/%s/%d: calls %v, want %v", want.Set, want.Sort, want.Limit, fs.calls, want.Calls)
			}
			ids := []string{}
			for _, h := range hits {
				ids = append(ids, h.ID)
			}
			if !slices.Equal(ids, want.IDs) {
				t.Errorf("%s/%s/%d: ids %v, want %v", want.Set, want.Sort, want.Limit, ids, want.IDs)
			}
			answer := map[string]any{"s": model.List[model.Product](hits)}
			if d := diff("", out(t, answer), decode(t, want.Answer)); d != "" {
				t.Errorf("%s/%s/%d: %s", want.Set, want.Sort, want.Limit, d)
			}
		}
	}
	if i != wantSearch {
		t.Errorf("ran %d cases, want %d", i, wantSearch)
	}
	t.Logf("search_sort.json: %d cases run, 0 skipped", i)
}

// limitStore is the Search store of the Python script: it records how it was called.
type limitStore struct {
	store.Store
	products []model.Product
	calls    [][]any
}

func (s *limitStore) Search(_ context.Context, query string, limit int) ([]model.Product, error) {
	s.calls = append(s.calls, []any{query, json.Number(strconv.Itoa(limit))})
	return slices.Clone(s.products)[:min(limit, len(s.products))], nil
}
