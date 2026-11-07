package match

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// recipeLine is what ParseItem answers today for one real recipe ingredient line. This test only
// records the behaviour, so a later matcher change is deliberate: update the table on purpose.
// size is "base amount" or "" for none.
type recipeLine struct {
	line  string
	words []string
	size  string
}

// The quantity is 1 for every line: ParseItem reads a count only in the "2 x item" form, and a
// leading recipe amount ("500 g") becomes the size. The weak cases are known, not wanted:
//   - fed, bdt., tsk. and spsk. are not units: the number and the unit stay in the words.
//   - a parenthesised alternative stays in the words ("icebergsalat (alternativt hjertesalat)").
//   - a bare count ("1 æg", "2 æg") keeps the number as a word and has no size.
//   - filler words stay ("eller", "til", "fra", "godt").
var recipeLines = []recipeLine{
	// nemlig: tests/fixtures/nemlig/recipe_details.json (read from the file below).
	{"2 stk. rødløg", []string{"rødløg"}, "stk 2.00"},
	{"2 fed hvidløg", []string{"2", "fed", "hvidløg"}, ""},
	{"1 bdt. persille", []string{"1", "bdt", "persille"}, ""},
	{"500 g hakket grise- og kalvekød", []string{"hakket", "grise", "kalvekød"}, "g 500.00"},
	{"3 tsk. kebab krydderi", []string{"3", "tsk", "kebab", "krydderi"}, ""},
	{"1 spsk. neutral olie", []string{"1", "spsk", "neutral", "olie"}, ""},
	{"salt", []string{"salt"}, ""},
	{"peber", []string{"peber"}, ""},
	{"200 g tomater", []string{"tomater"}, "g 200.00"},
	{"1 stk. agurk", []string{"agurk"}, "stk 1.00"},
	{"1 stk. icebergsalat (alternativt hjertesalat)", []string{"icebergsalat", "alternativt", "hjertesalat"}, "stk 1.00"},
	{"1 spsk. olivenolie", []string{"1", "spsk", "olivenolie"}, ""},
	{"3 dl yoghurt naturel", []string{"yoghurt", "naturel"}, "ml 300.00"},
	{"1 tsk. sukker", []string{"1", "tsk", "sukker"}, ""},
	{"8 stk. fladbrød", []string{"fladbrød"}, "stk 8.00"},
	// BilkaToGo: the karbonader and carbonara payloads in tests/fixtures/bilkatogo/recipes/,
	// as store.Recipe returns their ingredient lines (copied, in page order).
	{"500 g hakket svin", []string{"hakket", "svin"}, "g 500.00"},
	{"1 æg", []string{"1", "æg"}, ""},
	{"1 løg", []string{"1", "løg"}, ""},
	{"Godt med salt og peber", []string{"godt", "salt", "peber"}, ""},
	{"2/3 dl mel", []string{"mel"}, "ml 66.67"},
	{"1 dl mælk", []string{"mælk"}, "ml 100.00"},
	{"2 æg", []string{"2", "æg"}, ""},
	{"2 dl rasp", []string{"rasp"}, "ml 200.00"},
	{"Salt, peber", []string{"salt", "peber"}, ""},
	{"1/2 dl olie eller smør til stegning", []string{"olie", "eller", "smør", "stegning"}, "ml 50.00"},
	{"300 g kartofler", []string{"kartofler"}, "g 300.00"},
	{"1 spsk. smør", []string{"1", "spsk", "smør"}, ""},
	{"1 spsk. mel", []string{"1", "spsk", "mel"}, ""},
	{"3 dl sødmælk", []string{"sødmælk"}, "ml 300.00"},
	{"1 dl ærter (fra frost)", []string{"ærter", "fra", "frost"}, "ml 100.00"},
	{"4 stk. grønne asparges", []string{"grønne", "asparges"}, "stk 4.00"},
	{"1 spsk. hakket persille", []string{"1", "spsk", "hakket", "persille"}, ""},
	{"150 g bacon eller pancetta", []string{"bacon", "eller", "pancetta"}, "g 150.00"},
	{"400 g spaghetti", []string{"spaghetti"}, "g 400.00"},
	{"3 æggeblommer", []string{"3", "æggeblommer"}, ""},
	{"70 g parmesanost", []string{"parmesanost"}, "g 70.00"},
	{"1 dl fløde", []string{"fløde"}, "ml 100.00"},
	{"½ dl kogevand fra pastaen", []string{"kogevand", "fra", "pastaen"}, "ml 50.00"},
	{"Ekstra parmesan til servering", []string{"ekstra", "parmesan", "servering"}, ""},
}

func TestRecipeLinesParseAsRecorded(t *testing.T) {
	for _, c := range recipeLines {
		q, ws, s := ParseItem(c.line)
		if q.CmpInt(1) != 0 {
			t.Errorf("%q: quantity %v, recorded 1", c.line, q)
		}
		if !slices.Equal(ws, c.words) {
			t.Errorf("%q: words %q, recorded %q", c.line, ws, c.words)
		}
		size := ""
		if s != nil {
			size = fmt.Sprintf("%s %v", s.Base, s.Amount)
		}
		if size != c.size {
			t.Errorf("%q: size %q, recorded %q", c.line, size, c.size)
		}
	}
}

// Every ingredient line of the nemlig fixture is in the table, so a refreshed fixture cannot
// add lines that nobody has looked at.
func TestNemligRecipeFixtureLinesAreRecorded(t *testing.T) {
	raw, err := os.ReadFile("../../tests/fixtures/nemlig/recipe_details.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Ingredients []struct {
			Ingredients []struct {
				Text string `json:"text"`
			} `json:"ingredients"`
		} `json:"ingredients"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, c := range recipeLines {
		known[c.line] = true
	}
	n := 0
	for _, g := range doc.Ingredients {
		for _, i := range g.Ingredients {
			n++
			if !known[strings.TrimSpace(i.Text)] {
				t.Errorf("fixture line %q is not in recipeLines", i.Text)
			}
		}
	}
	if n == 0 {
		t.Fatal("no ingredient lines in the fixture")
	}
}
