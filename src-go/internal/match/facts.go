package match

import (
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
)

// Frozen or canned. Never the default for fresh food: "blåbær" is fresh, "frosne blåbær" is not.
// "Cherrytomater i tomatjuice", Bilka "Kolonial > Konserves". nemlig puts jars in plain "Kolonial":
// "Appetit agurker" is pickled, never a fresh agurk.
var canned = []string{"konserv", "dåse", "juice", "kolonial"}

var preserved = []string{"olie", "lage", "marinade"} // last part of a name word: "krydderolie", not "Lagerøl"

var frozen = []string{"frost", "frosne", "frossen", "frosset"}

var fresh = []string{"frugt", "grønt", "bær", "grøntsag"} // frozen loses only here: frozen meat is fine

// Ready to eat: cold cuts, smoked, fried, breaded. Never raw meat unless the item asks for it.
var preparedWords = []string{"røget", "røgede", "grillet", "grillede", "stegt", "stegte", "pålæg",
	"indbagt", "indbagte", "paneret", "panerede"} // ready meals: "Indbagt laksefilet"

func anyIn(text string, list []string) bool {
	for _, m := range list {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// text is the name and the category in lower case, like Python's f"{name} {category}".lower().
func text(p *model.Product) string { return pystr.Lower(p.Name + " " + str(p.Category)) }

// nameEnds tells whether a word of the name ends with a PRESERVED part.
func nameEnds(p *model.Product) bool {
	for _, w := range words(p.Name) {
		for _, m := range preserved {
			if strings.HasSuffix(w, m) {
				return true
			}
		}
	}
	return false
}

// ProcessedRank is 1 for canned, preserved or frozen fruit and vegetables, when the item does not
// ask for it. `fresh` says the search also found the item fresh ('Grønt'), so the item is produce.
// Canned fish or frozen meat is not demoted: "Makrel i tomat" 125 g is a fine tin, and a
// frozen 35 kr offer is a fine 'kyllingelår'.
func ProcessedRank(wordList []string, p *model.Product, isFresh bool) int {
	t := text(p)
	item := strings.Join(wordList, " ")
	produce := isFresh || IsFresh(p)
	for _, w := range wordList {
		produce = produce || strings.HasSuffix(w, "bær")
	}
	if !produce {
		return 0
	}
	isCanned := anyIn(t, canned) && !anyIn(item, canned)
	isPreserved := nameEnds(p) && !anyIn(item, preserved)
	isFrozen := anyIn(t, frozen) && !anyIn(item, frozen)
	if isCanned || isPreserved || isFrozen {
		return 1
	}
	return 0
}

// IsFresh: fruit or vegetables, not canned or frozen: nemlig 'Grønt', Bilka 'Frugt & grønt'.
func IsFresh(p *model.Product) bool {
	t := text(p)
	return anyIn(t, fresh) && !anyIn(t, canned) && !anyIn(t, frozen)
}

// Processed is 'frost' or 'konserves' when the name or category says so, else nil. A fact for the
// caller, not a rank: plain 'Kolonial' (nemlig's dry goods too) is not konserves here.
func Processed(p *model.Product) *string {
	t := text(p)
	if anyIn(t, frozen) {
		s := "frost"
		return &s
	}
	if anyIn(t, canned[:3]) || nameEnds(p) { // canned without "kolonial"
		s := "konserves"
		return &s
	}
	return nil
}

// IsOeko: øko, økologisk or organic as a word. Not "Økonomi".
func IsOeko(p *model.Product) bool { return hasOeko(p.Name + " " + str(p.Brand)) }

// Prepared is true for 'Røget kyllingebryst' (Bilka 'Pålæg' category) when the item does not ask for
// it: 'kyllingebryst 450 g' is raw meat, 'røget laks' asks.
func Prepared(wordList []string, p *model.Product) bool {
	return marked(text(p)) && !marked(strings.Join(wordList, " "))
}
