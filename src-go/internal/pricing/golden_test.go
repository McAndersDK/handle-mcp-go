package pricing_test

import (
	"encoding/json"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/goldentest"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pricing"
)

var wantRows = map[string]int{"nemlig": 995, "bilkatogo": 789, "synthetic": 649}
var wantCorpus = map[string]int{"nemlig": 131, "bilkatogo": 101, "synthetic": 82}

const wantItems = 171

type pairRow struct {
	P       int
	Packs   *json.Number
	Pieces  *json.Number
	Count   json.Number
	TooMuch *bool                `json:"too_much"`
	ByDay   map[string]dayResult `json:"by_day"`
}

type dayResult struct {
	Cost    string
	PerUnit [2]json.Number `json:"per_unit"`
}

func qty(t *testing.T, n json.Number) match.Qty {
	t.Helper()
	q, ok := match.ParseQty(n.String())
	if !ok {
		t.Fatalf("not a count: %s", n)
	}
	return q
}

func checkCount(t *testing.T, label string, got match.Qty, ok bool, want *json.Number) {
	t.Helper()
	switch {
	case want == nil && ok:
		t.Errorf("%s: got %s, want none", label, got)
	case want != nil && !ok:
		t.Errorf("%s: got none, want %s", label, *want)
	case want != nil && got.String() != want.String():
		t.Errorf("%s: got %s, want %s", label, got, *want)
	}
}

func TestGoldenPairs(t *testing.T) {
	days := map[string]*model.Date{"null": nil}
	for _, s := range []string{"2026-11-02", "2030-01-01"} {
		d, err := model.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		days[s] = &d
	}
	rows, dayCases := 0, 0
	for name, wantRowCount := range wantRows {
		corpus := goldentest.Corpus(t, name)
		var g struct {
			Days  []string
			Items []string
			Pairs map[string][]pairRow
		}
		goldentest.Read(t, "match/pairs_"+name+".json", &g)
		if len(g.Items) != wantItems || len(corpus) != wantCorpus[name] || len(g.Days) != 3 {
			t.Fatalf("%s: %d items, %d products, days %v", name, len(g.Items), len(corpus), g.Days)
		}
		n := 0
		for _, item := range g.Items {
			quantity, _, size := match.ParseItem(item)
			for _, r := range g.Pairs[item] {
				p := &corpus[r.P]
				label := name + ": " + item + " x " + p.ID + " " + p.Name
				packs, okPacks := pricing.Packs(size, quantity, p)
				checkCount(t, label+" packs", packs, okPacks, r.Packs)
				pieces, okPieces := pricing.Pieces(size, quantity, p)
				checkCount(t, label+" pieces", pieces, okPieces, r.Pieces)
				count := quantity // packs or pieces or quantity
				if okPacks {
					count = packs
				} else if okPieces {
					count = pieces
				}
				if count.String() != r.Count.String() {
					t.Errorf("%s: count %s, want %s", label, count, r.Count)
				}
				if size == nil {
					if r.TooMuch != nil {
						t.Errorf("%s: golden has too_much for an item with no size", label)
					}
				} else if r.TooMuch == nil || pricing.TooMuch(size, quantity, count, p) != *r.TooMuch {
					t.Errorf("%s: too_much %v, want %v", label, pricing.TooMuch(size, quantity, count, p), r.TooMuch)
				}
				if len(r.ByDay) != 3 {
					t.Fatalf("%s: %d days", label, len(r.ByDay))
				}
				for key, want := range r.ByDay {
					on, known := days[key]
					if !known {
						t.Fatalf("%s: unknown day %q", label, key)
					}
					if got := pricing.Cost(p, count, on); got.Cmp(goldentest.Money(t, want.Cost)) != 0 {
						t.Errorf("%s: cost on %s %s, want %s", label, key, got, want.Cost)
					}
					u := pricing.PerUnit(p, quantity, count, on)
					wantKind := qty(t, want.PerUnit[0])
					// per_unit is rounded half up to 10 places in the golden
					wantValue := goldentest.Money(t, want.PerUnit[1].String())
					if u.Kind != int(wantKind.Big().Int64()) || u.Value.Round(10).Cmp(wantValue) != 0 {
						t.Errorf("%s: per_unit on %s (%d, %s), want (%s, %s)", label, key, u.Kind, u.Value.Round(10).FormatG(), want.PerUnit[0], want.PerUnit[1])
					}
					dayCases++
				}
				n++
			}
		}
		if n != wantRowCount {
			t.Errorf("%s: %d rows checked, want %d", name, n, wantRowCount)
		}
		rows += n
	}
	t.Logf("pairs_*.json: %d rows run (packs, pieces, too_much), %d cost + per_unit day cases, 0 skipped", rows, dayCases)
}
