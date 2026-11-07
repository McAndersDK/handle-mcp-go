package match_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/goldentest"
	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// Expected case counts. A golden that shrinks, or a case the test does not read, fails.
const (
	wantItems     = 171
	wantSizes     = 522
	wantProducts  = 314 // nemlig 131 + bilkatogo 101 + synthetic 82
	wantPairItems = 171 // items per corpus
)

var wantRows = map[string]int{"nemlig": 995, "bilkatogo": 789, "synthetic": 649}
var wantCorpus = map[string]int{"nemlig": 131, "bilkatogo": 101, "synthetic": 82}

// sizeJSON is a golden size: [base, amount] or null.
func checkSize(t *testing.T, what string, got *match.Size, want []string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s: got %v, want no size", what, got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: got no size, want %v", what, want)
		return
	}
	if got.Base != want[0] || got.Amount.Cmp(goldentest.Money(t, want[1])) != 0 {
		t.Errorf("%s: got %s %s, want %v", what, got.Base, got.Amount.FormatG(), want)
	}
}

func TestGoldenParse(t *testing.T) {
	var g struct {
		Items map[string]struct {
			Asked    []string
			Quantity json.Number
			Query    string
			Size     []string
			Words    []string
		}
		ParseSize map[string][]string `json:"parse_size"`
		Products  map[string][]struct {
			IsFresh       bool     `json:"is_fresh"`
			IsOeko        bool     `json:"is_oeko"`
			PackSize      []string `json:"pack_size"`
			ParseSizeName []string `json:"parse_size_name"`
			ParseSizeSize []string `json:"parse_size_size"`
			Processed     *string
		}
	}
	goldentest.Read(t, "match/parse.json", &g)
	if len(g.Items) != wantItems || len(g.ParseSize) != wantSizes {
		t.Fatalf("parse.json has %d items and %d sizes, want %d and %d", len(g.Items), len(g.ParseSize), wantItems, wantSizes)
	}
	for item, want := range g.Items {
		q, words, size := match.ParseItem(item)
		if q.String() != want.Quantity.String() {
			t.Errorf("%q: quantity %s, want %s", item, q, want.Quantity)
		}
		if !reflect.DeepEqual(append([]string{}, words...), append([]string{}, want.Words...)) {
			t.Errorf("%q: words %q, want %q", item, words, want.Words)
		}
		if got := match.Query(item); got != want.Query {
			t.Errorf("%q: query %q, want %q", item, got, want.Query)
		}
		asked := match.Asked(item)
		if !sort.StringsAreSorted(asked) || !reflect.DeepEqual(append([]string{}, asked...), append([]string{}, want.Asked...)) {
			t.Errorf("%q: asked %q, want %q", item, asked, want.Asked)
		}
		checkSize(t, item+" (item size)", size, want.Size)
	}
	for text, want := range g.ParseSize {
		checkSize(t, text+" (parse_size)", match.ParseSize(text), want)
	}
	products := 0
	for name, facts := range g.Products {
		corpus := goldentest.Corpus(t, name)
		if len(facts) != wantCorpus[name] || len(corpus) != wantCorpus[name] {
			t.Fatalf("%s: %d facts, %d products, want %d", name, len(facts), len(corpus), wantCorpus[name])
		}
		for i, want := range facts {
			p := &corpus[i]
			label := name + "/" + p.ID + " " + p.Name
			if got := match.IsFresh(p); got != want.IsFresh {
				t.Errorf("%s: is_fresh %v, want %v", label, got, want.IsFresh)
			}
			if got := match.IsOeko(p); got != want.IsOeko {
				t.Errorf("%s: is_oeko %v, want %v", label, got, want.IsOeko)
			}
			got, wantP := match.Processed(p), want.Processed
			if (got == nil) != (wantP == nil) || (got != nil && *got != *wantP) {
				t.Errorf("%s: processed %v, want %v", label, got, wantP)
			}
			checkSize(t, label+" pack_size", match.PackSize(p), want.PackSize)
			checkSize(t, label+" name size", match.ParseSize(p.Name), want.ParseSizeName)
			if p.Size != nil {
				checkSize(t, label+" size field", match.ParseSize(*p.Size), want.ParseSizeSize)
			} else if want.ParseSizeSize != nil {
				t.Errorf("%s: golden has a size for a missing size field", label)
			}
			products++
		}
	}
	if products != wantProducts {
		t.Errorf("checked %d products, want %d", products, wantProducts)
	}
	t.Logf("parse.json: %d items, %d sizes, %d products run, 0 skipped", len(g.Items), len(g.ParseSize), products)
}

type pairRow struct {
	P                  int
	Score              float64
	Prepared           bool
	ProcessedRank      int `json:"processed_rank"`
	ProcessedRankFresh int `json:"processed_rank_fresh"`
	Mixed              int
	FlavourOnly        bool `json:"flavour_only"`
	BrandOnly          bool `json:"brand_only"`
	CategoryRank       int  `json:"category_rank"`
}

func TestGoldenPairs(t *testing.T) {
	rows, absent := 0, 0
	for name, wantRowCount := range wantRows {
		corpus := goldentest.Corpus(t, name)
		var g struct {
			Items []string
			Pairs map[string][]pairRow
		}
		goldentest.Read(t, "match/pairs_"+name+".json", &g)
		if len(g.Items) != wantPairItems || len(corpus) != wantCorpus[name] {
			t.Fatalf("%s: %d items, %d products", name, len(g.Items), len(corpus))
		}
		n := 0
		for _, item := range g.Items {
			_, words, _ := match.ParseItem(item)
			asked := match.Asked(item)
			listed := map[int]pairRow{}
			for _, r := range g.Pairs[item] {
				listed[r.P] = r
			}
			for i := range corpus {
				p := &corpus[i]
				got := match.Score(words, p, asked)
				r, ok := listed[i]
				if !ok { // sparse file: a pair that is not listed scores 0
					if got != 0 {
						t.Errorf("%s: %q x %s %q scores %v, golden has no row (0)", name, item, p.ID, p.Name, got)
					}
					absent++
					continue
				}
				n++
				label := name + ": " + item + " x " + p.ID + " " + p.Name
				if got != r.Score {
					t.Errorf("%s: score %v, want %v", label, got, r.Score)
				}
				if v := match.Prepared(words, p); v != r.Prepared {
					t.Errorf("%s: prepared %v, want %v", label, v, r.Prepared)
				}
				if v := match.ProcessedRank(words, p, false); v != r.ProcessedRank {
					t.Errorf("%s: processed_rank %v, want %v", label, v, r.ProcessedRank)
				}
				if v := match.ProcessedRank(words, p, true); v != r.ProcessedRankFresh {
					t.Errorf("%s: processed_rank fresh %v, want %v", label, v, r.ProcessedRankFresh)
				}
				if v := match.Mixed(words, p); v != r.Mixed {
					t.Errorf("%s: mixed %v, want %v", label, v, r.Mixed)
				}
				if v := match.FlavourOnly(words, p); v != r.FlavourOnly {
					t.Errorf("%s: flavour_only %v, want %v", label, v, r.FlavourOnly)
				}
				if v := match.BrandOnly(words, p); v != r.BrandOnly {
					t.Errorf("%s: brand_only %v, want %v", label, v, r.BrandOnly)
				}
				if v := match.CategoryRank(words, p); v != r.CategoryRank {
					t.Errorf("%s: category_rank %v, want %v", label, v, r.CategoryRank)
				}
			}
		}
		if n != wantRowCount {
			t.Errorf("%s: %d rows checked, want %d", name, n, wantRowCount)
		}
		rows += n
	}
	if rows != 995+789+649 {
		t.Errorf("checked %d rows", rows)
	}
	t.Logf("pairs_*.json: %d listed rows run, %d unlisted pairs asserted 0, 0 skipped", rows, absent)
}

var _ = model.Product{}
