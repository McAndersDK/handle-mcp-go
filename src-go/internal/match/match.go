// Package match matches a shopping-list item to a store product, by words only. No money here.
//
// It is a port of match.py (ADR 0005); the golden files in tests/golden/match are the check.
//
//   - "2 x rugbrød" means quantity 2. A size like "1,5 kg" is the amount wanted.
//   - A product matches well when every word of the item is a whole word in its name.
//   - A word only after "m.", "m", "med", "i", "u." ... is a flavour, not the product: "Yoghurt m. banan"
//     is not a banana. Such products never win, unless the item asks for it: "makrel i tomat".
//   - A word only in the brand counts half: "Fior di Frutta" jam is not "fior di latte".
//   - "makrelfilet" and "makrelstykker" are makrel: a cut of the thing named first. Two cuts of one
//     thing are the same ("laksestykke" for "laksefilet"), and so is a part ("kyllingeoverlår").
//   - Canned, preserved or frozen produce ("Hvidløg i krydderolie") loses when the search also found
//     it fresh. Canned fish and frozen meat do not lose: "Makrel i tomat" is a tin.
//   - A blend ("m. 50% grønt") loses to the plain product and is never sure.
//   - Danish: c ~ k, singular ~ plural, compounds name the thing last ("frilandsæg" is æg).
//
// Lengths, slices and suffixes count characters (runes), not bytes: "æg" has length 2.
package match

import (
	"sort"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
)

var stopWords = map[string]bool{"og": true, "med": true, "i": true, "af": true, "en": true, "et": true, "til": true, "uden": true}

type unit struct {
	name   string
	base   string
	factor int64
}

// units are in the order of the pattern in SIZE_RE.
var units = []unit{
	{"g", "g", 1}, {"gr", "g", 1}, {"kg", "g", 1000},
	{"ml", "ml", 1}, {"cl", "ml", 10}, {"dl", "ml", 100},
	{"l", "ml", 1000}, {"ltr", "ml", 1000}, {"liter", "ml", 1000},
	{"stk", "stk", 1}, {"st", "stk", 1},
	{"ruller", "rl", 1}, {"rulle", "rl", 1}, {"rl", "rl", 1}, // rolls, not 42 wet wipes
}

// IsPiece tells whether a base unit is counted (PIECES): '2 x køkkenrulle' is 2 packs of rolls, like pieces.
func IsPiece(base string) bool { return base == "stk" || base == "rl" }

// Size is an amount in a base unit: "1 l" is {"ml", 1000}.
type Size struct {
	Base   string
	Amount model.Money
}

// ParseSize: '1 l' → ml 1000. '12 x 0,25 l' → ml 3000. nil if no size.
func ParseSize(text string) *Size {
	m, ok := searchSize([]rune(text))
	if !ok {
		return nil
	}
	return sizeOf(m)
}

func sizeOf(m sizeMatch) *Size {
	u := unitByName[pystr.Lower(m.unit)]
	amount, err := model.ParseMoney(strings.ReplaceAll(pystr.ASCIIDigits(m.amount), ",", "."))
	if err != nil || u.name == "" {
		return nil
	}
	amount = amount.Mul(u.factor)
	if m.hasCount { // "0 x 5 g" is 0 g: a count that is there is used
		n, ok := ParseQty(m.count)
		if !ok {
			return nil
		}
		amount = amount.MulMoney(n.Money())
	}
	return &Size{Base: u.base, Amount: amount}
}

// ParseItem: '2 x letmælk 1 l' → 2, ['letmælk'], ml 1000.
func ParseItem(item string) (Qty, []string, *Size) {
	quantity := QtyOf(1)
	if digits, end, ok := matchQty([]rune(item)); ok {
		quantity, _ = ParseQty(digits)
		item = string([]rune(item)[end:])
	}
	size := ParseSize(item)
	var ws []string
	for _, w := range words(subSize(item, " ")) {
		if !stopWords[w] {
			ws = append(ws, w)
		}
	}
	if quantity.CmpInt(1) < 0 {
		quantity = QtyOf(1)
	}
	return quantity, ws, size
}

// Query is what we send to the store's search: the words only. '2 x letmælk 1 l' → 'letmælk'.
func Query(item string) string {
	_, ws, _ := ParseItem(item)
	if q := strings.Join(ws, " "); q != "" {
		return q
	}
	return item
}

// Words is _words: the lower-case words of a text.
func Words(text string) []string { return words(text) }

// Asked: words the item itself puts after 'i', 'med' ...: 'makrel i tomat' asks for tomat.
// Sorted, without doubles.
func Asked(item string) []string {
	_, after, found := splitFlavour(" " + subSize(item, " ") + " ")
	if !found {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, w := range words(after) {
		if !stopWords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

// PackSize: the size field, else the name. Rolls in the name win over 'stk' in the size field:
// nemlig sells 'Toiletpapir 8 ruller' with size '8 stk'.
func PackSize(p *model.Product) *Size {
	var size *Size
	if p.Size != nil {
		size = ParseSize(*p.Size)
	}
	name := ParseSize(p.Name)
	if name != nil && name.Base == "rl" && (size == nil || size.Base == "stk") {
		return name
	}
	if size != nil {
		return size
	}
	return name
}

// str is a string pointer's value; "" when nil (Python: `x or ""`).
func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var unitByName = func() map[string]unit {
	m := map[string]unit{}
	for _, u := range units {
		m[u.name] = u
	}
	return m
}()
