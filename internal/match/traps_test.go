package match

import (
	"reflect"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// Hand tests for the places where Go and Python differ (the traps of the port). The goldens
// cover most of them too; these say why.

func product(name string) *model.Product {
	p := model.NewProduct("t", "1", name, model.NewMoney(1, 0))
	return &p
}

func TestLengthsAreCharacters(t *testing.T) {
	// "æg" is 2 characters but 3 bytes. Byte lengths would make each of these true.
	if plural("æg", "æge") {
		t.Error("plural: 'æg' is too short (< 3 characters) to take an ending")
	}
	if !plural("pære", "pærer") || !plural("banan", "bananer") {
		t.Error("plural: pære, banan")
	}
	if compound("ø", "æbø") { // 3 characters is not 1 + 3
		t.Error("compound: 'æbø' is only 2 characters longer than 'ø'")
	}
	if !compound("æg", "frilandsæg") || !compound("æg", "hønseæg") {
		t.Error("compound: frilandsæg, hønseæg")
	}
	if compound("ost", "post") || !compound("ost", "hytteost") {
		t.Error("compound: 'post' is not 'ost', 'hytteost' is")
	}
	if stemHit("æggehvide", []string{"ægg"}) { // 'ægg' is 3 characters, 5 bytes
		t.Error("stemHit: a token needs 4 characters")
	}
	if !stemHit("æggehvide", []string{"ægge"}) {
		t.Error("stemHit: ægge")
	}
	if suffix("xyæg", "yæg") { // 'yæg' is 3 characters, 4 bytes
		t.Error("suffix: both sides need 4 characters")
	}
	if !suffix("cherrytomater", "tomater") {
		t.Error("suffix: cherrytomater")
	}
	// the slice before 'lår' is by characters: 'kyllingelår' - 'lår' = 'kyllinge'
	if !described("kyllingelår", "kyllingeoverlår") || !described("kyllingelår", "kyllingeunderlår") {
		t.Error("described: kyllingeoverlår")
	}
	if described("kartofler", "kartoverofler") {
		t.Error("described: only before 'lår'")
	}
	if !described("laksefilet", "laksestykke") || !described("æg", "frilandsæg") || !described("makrel", "makrelfilet") {
		t.Error("described: cuts and describers")
	}
}

func TestScoreDanish(t *testing.T) {
	cases := []struct {
		words []string
		name  string
		want  float64
	}{
		{[]string{"æg"}, "Frilandsæg", 1}, // a describer
		{[]string{"æg"}, "Hønseæg", 0.75}, // a compound
		{[]string{"ost"}, "Post", 0.5},    // a word inside a word
		{[]string{"ost"}, "Ost", 1},
		{[]string{"ost"}, "Hytteost", 0.75},
		{[]string{"clementiner"}, "Klementiner", 1}, // c ~ k
		{[]string{"banan"}, "Bananer", 1},
		{[]string{"banan"}, "Yoghurt m. banan", 0.25}, // only in the flavour part
		{[]string{"banan"}, "Yoghurt m banan", 0.25},
		{[]string{"mælk"}, "ØKO MÆLK", 1}, // upper case Danish letters
		{nil, "Mælk", 0},
	}
	for _, c := range cases {
		if got := Score(c.words, product(c.name), nil); got != c.want {
			t.Errorf("Score(%q, %q) = %v, want %v", c.words, c.name, got, c.want)
		}
	}
	if got := Score([]string{"tomat"}, product("Makrel i tomat"), []string{"tomat"}); got != 1 {
		t.Errorf("asked flavour word: %v", got)
	}
}

func TestUnicodeBoundaries(t *testing.T) {
	// \b is Unicode in Python: 'æ' and 'ø' are word characters.
	oeko := map[string]bool{
		"Økologisk mælk": true, "ØKO æg": true, "Æbler øko.": true, "organic": true, "ORGANIC": true,
		"Økologiske æg": true, "Økonomi": false, "Økoæ": false, "æøko": false, "Biøko": false,
		"Økologiskx": false, "": false,
	}
	for text, want := range oeko {
		if got := hasOeko(text); got != want {
			t.Errorf("hasOeko(%q) = %v, want %v", text, got, want)
		}
	}
	marks := map[string]bool{
		"røget laks": true, "kyllingepålæg": true, "pålæg": true, "grønrøget": true,
		"xpålægæ": false, "kyllingepålægsmix": false, "bacon": false, "": false,
	}
	for text, want := range marks {
		if got := marked(text); got != want {
			t.Errorf("marked(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestSizeUnicode(t *testing.T) {
	cases := []struct {
		text     string
		wantBase string
		want     string // "" for no size
	}{
		{"1 l", "ml", "1000"},
		{"1 l", "ml", "1000"},         // a non-breaking space is \s in Python
		{"mælk ١ l", "ml", "1000"},    // Arabic-Indic digit
		{"12×0,25 l", "ml", "3000"},   // ×
		{"12 X 0,25 L", "ml", "3000"}, // ignore case
		{"2 x 1 KG", "g", "2000"},     // ignore case
		{"1.000 g", "g", "1"},         // a decimal point
		{"1,5,5 kg", "g", "5500"},     // the first number that fits
		{"0 x 5 g", "g", "0"},         // a count that is there is used
		{"1l mælk", "ml", "1000"},     // no space
		{"kyllingebryst 450g", "g", "450"},
		{"2 stkx", "", ""}, // \b: 'stkx' is no unit
		{"5 liters", "", ""},
		{"5 gram", "", ""},
		{"Kg mel", "", ""},
		{"٢ x mælk", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		got := ParseSize(c.text)
		if c.want == "" {
			if got != nil {
				t.Errorf("ParseSize(%q) = %v, want none", c.text, got)
			}
			continue
		}
		if got == nil || got.Base != c.wantBase || got.Amount.Cmp(mustMoney(t, c.want)) != 0 {
			t.Errorf("ParseSize(%q) = %v, want %s %s", c.text, got, c.wantBase, c.want)
		}
	}
}

func mustMoney(t *testing.T, s string) model.Money {
	t.Helper()
	m, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseItemQuantity(t *testing.T) {
	cases := []struct {
		item  string
		qty   string
		words []string
	}{
		{"2 x letmælk 1 l", "2", []string{"letmælk"}},
		{"2 x mælk", "2", []string{"mælk"}},
		{"٢ x mælk", "2", []string{"mælk"}},
		{"2 X mælk", "2", []string{"mælk"}},
		{"2 × mælk", "2", []string{"mælk"}},
		{"2×mælk", "1", []string{"2", "mælk"}}, // \s+ is needed after the x
		{"2 x 5 l", "1", nil},                  // the lookahead (?=\D): a digit follows
		{"2 x  5 l", "2", nil},                 // two spaces: the match gives one back
		{"  2 x mælk", "2", []string{"mælk"}},
		{"0 x mælk", "1", []string{"mælk"}}, // max(quantity, 1)
		{"3 x 2 x mælk", "1", []string{"3", "x", "2", "x", "mælk"}},
		{"99999999999999999999 x mælk", "99999999999999999999", []string{"mælk"}},
		{"İs", "1", []string{"s"}}, // Python: "İ".lower() is "i" + a dot: the word 'i' is a stop word
		{"MÆLK", "1", []string{"mælk"}},
		{"makrel i tomat", "1", []string{"makrel", "tomat"}},
	}
	for _, c := range cases {
		q, words, _ := ParseItem(c.item)
		if q.String() != c.qty || !reflect.DeepEqual(words, c.words) {
			t.Errorf("ParseItem(%q) = %s %q, want %s %q", c.item, q, words, c.qty, c.words)
		}
	}
}

func TestFlavourSplit(t *testing.T) {
	cases := map[string][2]string{
		"Yoghurt m. banan":      {"Yoghurt", "banan"},
		"Yoghurt m banan":       {"Yoghurt", "banan"},
		"Yoghurt M. banan":      {"Yoghurt", "banan"},
		"Laks med skind":        {"Laks", "skind"},
		"Te SMAG af mandarin":   {"Te", "af mandarin"},
		"Pasta u. æg":           {"Pasta", "æg"},
		"Makrel i tomat i olie": {"Makrel", "tomat i olie"}, // maxsplit=1
	}
	for in, want := range cases {
		before, after, found := splitFlavour(in)
		if !found || before != want[0] || after != want[1] {
			t.Errorf("splitFlavour(%q) = %q, %q, %v", in, before, after, found)
		}
	}
	for _, in := range []string{"Mælk", "Medister", "Imam", "m.", "Yoghurt m.banan", " m"} {
		if _, _, found := splitFlavour(in); found {
			t.Errorf("splitFlavour(%q) must not match", in)
		}
	}
}

func TestMixed(t *testing.T) {
	cases := []struct {
		name  string
		words []string
		want  int
	}{
		{"Hakket oksekød m. 50% grønt", []string{"hakket", "oksekød"}, 1},
		{"Hakket oksekød m. 50 % grønt", []string{"hakket", "oksekød"}, 1},
		{"Hakket oksekød m. 50% grønt", []string{"grønt"}, 0},  // the item asks for it
		{"Hakket oksekød m. 0,5% fedt", []string{"hakket"}, 0}, // fat is not a blend
		{"Hakket oksekød m. 5% FEDT", []string{"hakket"}, 0},
		{"Rugbrød m. kerner", []string{"rugbrød"}, 0},
		{"Hakket oksekød 50% grønt", []string{"hakket"}, 1}, // a share anywhere in the name
		{"HK. OKSEKØD, 35% GRØNT", []string{"hakket", "oksekød"}, 1},
		{"Mix 50% grønt", []string{"grønt"}, 0},
		{"HK. GRISEKØD 8-12%", []string{"hakket", "grisekød"}, 0},
		{"Hakket oksekød 14-18%", []string{"hakket", "oksekød"}, 0},
		{"Letmælk 1,5%", []string{"letmælk"}, 0},
		{"Mørk chokolade 70% kakao", []string{"chokolade"}, 0},
		{"Øl 4,6%", []string{"øl"}, 0},
		{"Mælk 3,5% mælk fat", []string{"mælk"}, 0},
		{"Hakket oksekød 4-7% fedt", []string{"hakket"}, 0},
	}
	for _, c := range cases {
		if got := Mixed(c.words, product(c.name)); got != c.want {
			t.Errorf("Mixed(%q, %q) = %d, want %d", c.name, c.words, got, c.want)
		}
	}
}

func TestPackSizeRolls(t *testing.T) {
	p := product("Toiletpapir 8 ruller")
	p.Size = ptrTo("8 stk")
	if got := PackSize(p); got == nil || got.Base != "rl" {
		t.Errorf("rolls in the name win over stk in the size field: %v", got)
	}
}

func ptrTo(s string) *string { return &s }

func TestQtyDivMod(t *testing.T) {
	q, _ := ParseQty("100000000000000000007")
	d, m := q.DivMod(QtyOf(10))
	if d.String() != "10000000000000000000" || m.String() != "7" {
		t.Errorf("DivMod = %s, %s", d, m)
	}
	if QtyOf(3).Min(QtyOf(5)).String() != "3" || QtyOf(5).Min(QtyOf(3)).String() != "3" {
		t.Error("Min")
	}
}

// REMA 1000 writes "HK." for "hakket" (Go-only, ADR 0005).
func TestHkMeansHakket(t *testing.T) {
	if got := words("HK. GRISEKØD 8-12%"); !reflect.DeepEqual(got, []string{"hakket", "grisekød", "8", "12"}) {
		t.Errorf("words: %q", got)
	}
	if got := words("Hkl hk2 shk"); !reflect.DeepEqual(got, []string{"hkl", "hk2", "shk"}) {
		t.Errorf("a word that contains hk must not change: %q", got)
	}
	if got := Score([]string{"hakket", "grisekød"}, product("HK. GRISEKØD 8-12%"), nil); got <= 0.5 {
		t.Errorf("HK. GRISEKØD should match hakket grisekød, score %v", got)
	}
	// The comma form has no flavour marker, but a share of another food anywhere in the name is a blend.
	// Pinned so a change is noticed (ADR 0005).
	if got := Mixed([]string{"hakket", "oksekød"}, product("HK. OKSEKØD, 35% GRØNT")); got != 1 {
		t.Errorf("Mixed = %d, want 1 (blend share in the name)", got)
	}
}
