package livelist

// Offline tests of the list and the checker (check_test.go): port of the offline half of
// upstream/tests/test_live_list.py.

import (
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/candidates"
	"github.com/McAndersDK/handle-mcp-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/server"
)

// --- offline: the list and the checker -----------------------------------------------------------

func mustList(t *testing.T) []Entry {
	t.Helper()
	list, err := loadList()
	if err != nil {
		t.Fatalf("%s: %v", listPath, err)
	}
	return list
}

func TestListIsAbout50UniqueItems(t *testing.T) {
	list := mustList(t)
	if len(list) < 45 || len(list) > 60 {
		t.Fatalf("%d items", len(list))
	}
	seen := map[string]bool{}
	for _, e := range list {
		if seen[e.Item] {
			t.Errorf("duplicate item %q", e.Item)
		}
		seen[e.Item] = true
		if _, words, _ := match.ParseItem(e.Item); len(words) == 0 {
			t.Errorf("%q: the store gets no words to search for", e.Item)
		}
		if len(e.Has) == 0 {
			t.Errorf("%q: no has words", e.Item)
		}
		for _, s := range e.Stores {
			if s != "nemlig" && s != "bilkatogo" {
				t.Errorf("%q: unknown store %q", e.Item, s)
			}
		}
	}
	for _, s := range storeNames {
		if n := len(itemsFor(list, s)); n < 45 {
			t.Errorf("%s has %d items", s, n)
		}
	}
}

func TestStoreNamesAreTheRegistry(t *testing.T) {
	reg := server.Registry()
	if len(reg) != len(storeNames) {
		t.Fatalf("registry %d stores, list %d", len(reg), len(storeNames))
	}
	for _, n := range storeNames {
		if _, ok := reg[n]; !ok {
			t.Errorf("%s is not in the registry", n)
		}
	}
}

func money(t *testing.T, s string) model.Money {
	t.Helper()
	m, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type copt func(*candidates.Candidate)

func suggested(c *candidates.Candidate) { c.Suggested = true }
func oeko(c *candidates.Candidate)      { c.Oeko = true }
func brand(b string) copt               { return func(c *candidates.Candidate) { c.Brand = &b } }
func category(s string) copt            { return func(c *candidates.Candidate) { c.Category = &s } }

func cand(t *testing.T, name, total string, opts ...copt) candidates.Candidate {
	c := candidates.Candidate{Name: name, LineTotal: money(t, total)}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// line is a BasketLine whose suggestion is the first candidate.
func line(guess bool, cands ...candidates.Candidate) *compare.BasketLine {
	first := cands[0]
	p := model.NewProduct("x", "1", first.Name, first.LineTotal)
	p.Brand = first.Brand
	first.Suggested = true
	all := append([]candidates.Candidate{first}, cands[1:]...)
	return &compare.BasketLine{
		Item: "x", Product: &p, LineTotal: first.LineTotal, Sure: true, Guess: guess,
		Candidates: all, Oeko: first.Oeko,
	}
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s: got %q, want %q", what, got, want)
	}
}

func TestCheckerFindsWrongWordsAndPrice(t *testing.T) {
	ten := money(t, "10")
	garlic := Entry{Item: "hvidløg", Has: []string{"hvidløg"}, Not: []string{"olie"}, MaxKr: &ten}
	eq(t, "garlic ok", problems(garlic, cand(t, "Hvidløg 1 stk", "4")), nil)
	eq(t, "garlic oil", problems(garlic, cand(t, "Hvidløg i olie", "4")), []string{"har 'olie'"})
	eq(t, "garlic dear", problems(garlic, cand(t, "Hvidløg 3 stk", "12")), []string{"12.00 kr. > 10.00 kr."})
	jam := Entry{Item: "fior di latte", Has: []string{"fior di latte|mozzarella"}}
	eq(t, "mozzarella", problems(jam, cand(t, "Mozzarella 125 g", "10")), nil)
	eq(t, "jam", problems(jam, cand(t, "Jordbær", "10", brand("Fior di Frutta"))),
		[]string{"mangler 'fior di latte|mozzarella'"})
	cheese := Entry{Item: "ost", Has: []string{"ost"}, Not: []string{"olie"}}
	eq(t, "skiveost", problems(cheese, cand(t, "Skiveost 45+", "10")), nil)
	eq(t, "leverpostej", problems(cheese, cand(t, "Leverpostej", "10")), []string{"mangler 'ost'"})
	eq(t, "krydderolie", problems(cheese, cand(t, "Ost i krydderolie", "10")), []string{"har 'olie'"})
	eq(t, "cherrytomater", problems(Entry{Item: "t", Has: []string{"tomat"}}, cand(t, "Cherrytomater", "10")), nil)
	stryhn := Entry{Item: "leverpostej", Has: []string{"stryhn"}}
	eq(t, "stryhn", problems(stryhn, cand(t, "Leverpostej bacon-peber", "10", brand("Stryhn's"))), nil)
}

func TestWordRuleIsUnicodeAware(t *testing.T) {
	for _, c := range []struct {
		word, text string
		want       bool
	}{
		{"kylling", "kyllingebryst", true},
		{"olie", "ost i krydderolie", true},
		{"ris", "friske grøntsager", false},
		{"ost", "leverpostej", false},
		{"æg", "økologiske æg 10 stk", true},
		{"æg", "bægre", false},
		{"majs", "babymajs", true},
		{"a|ost", "skiveost", true},
	} {
		if got := anyWord(c.word, c.text); got != c.want {
			t.Errorf("anyWord(%q, %q) = %v", c.word, c.text, got)
		}
	}
}

func TestFailsOnlyWhenNoCandidateFits(t *testing.T) {
	corn := Entry{Item: "majs", Has: []string{"majs"}, Not: []string{"baby"}}
	fail, warn := check(corn, line(false, cand(t, "Majs 3 stk", "10")))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, nil)
	// A wrong suggestion with a right candidate is a warning: the model can pick the other.
	fail, warn = check(corn, line(false, cand(t, "Babymajs", "10"), cand(t, "Majs 3 stk", "10")))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, []string{"har 'baby'"})
	fail, warn = check(corn, line(false, cand(t, "Babymajs", "10"), cand(t, "Babymajs øko", "10")))
	eq(t, "fail", fail, []string{"ingen kandidat passer (2 vist)", "har 'baby'"})
	eq(t, "warn", warn, nil)
	fail, warn = check(corn, nil)
	eq(t, "fail", fail, []string{"ikke fundet"})
	eq(t, "warn", warn, nil)
	fail, _ = check(corn, &compare.BasketLine{Item: "majs"})
	eq(t, "no product", fail, []string{"ikke fundet"})
}

func TestAGuessNeverCountsAsFitting(t *testing.T) {
	garlic := Entry{Item: "hvidløg", Has: []string{"hvidløg"}}
	fail, warn := check(garlic, line(true, cand(t, "Hvidløg", "10")))
	eq(t, "fail", fail, []string{"ingen kandidat passer (0 vist)", "gæt: Hvidløg"})
	eq(t, "warn", warn, nil)
}

func TestAGuessWithAFittingCandidateIsAWarning(t *testing.T) {
	garlic := Entry{Item: "hvidløg", Has: []string{"hvidløg"}}
	fail, warn := check(garlic, line(true, cand(t, "Løg", "10"), cand(t, "Hvidløg 1 stk", "10")))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, []string{"gæt: Løg"})
}

func TestOekoTooDearIsAWarning(t *testing.T) {
	entry := Entry{Item: "linser", Has: []string{"linser"}}
	plain := cand(t, "Røde linser", "20")
	fail, warn := check(entry, line(false, cand(t, "Røde linser øko", "22", oeko), plain))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, nil)
	fail, warn = check(entry, line(false, cand(t, "Røde linser øko", "27", oeko), plain))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, []string{"øko 27.00 kr. mod 20.00 kr."})
	tin := cand(t, "Røde linser", "9", category("Konserves"))
	canned := Entry{Item: "linser", Has: []string{"linser"}, Not: []string{"konserv"}}
	fail, warn = check(canned, line(false, cand(t, "Røde linser øko", "27", oeko), tin))
	eq(t, "fail", fail, nil)
	eq(t, "warn", warn, nil)
}
