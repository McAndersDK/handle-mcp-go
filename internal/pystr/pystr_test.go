package pystr_test

import (
	"testing"
	"unicode"

	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

func TestLenCountsCharacters(t *testing.T) {
	if pystr.Len("æg") != 2 || pystr.Len("frilandsæg") != 10 {
		t.Error("Len must count runes")
	}
}

func TestLowerDottedI(t *testing.T) {
	// Python: "İs".lower() is "i" + U+0307 + "s", not "is"
	if got := pystr.Lower("İs"); got != "i̇s" {
		t.Errorf("Lower(İs) = %q", got)
	}
	if got := pystr.Lower("ÆØÅ K"); got != "æøå k" {
		t.Errorf("Lower = %q", got)
	}
}

func TestDigits(t *testing.T) {
	for r, want := range map[rune]int{'7': 7, '٢': 2, '١': 1, '०': 0, '９': 9, '𝟓': 5} {
		if got, ok := pystr.DigitValue(r); !ok || got != want {
			t.Errorf("DigitValue(%q) = %d, %v", r, got, ok)
		}
	}
	if _, ok := pystr.DigitValue('x'); ok {
		t.Error("x is no digit")
	}
	if got := pystr.ASCIIDigits("٢,٥ kg"); got != "2,5 kg" {
		t.Errorf("ASCIIDigits = %q", got)
	}
	// every Nd range is a whole number of sets of ten, starting at zero
	for _, rg := range unicode.Nd.R16 {
		if (int(rg.Hi)-int(rg.Lo)+1)%10 != 0 || rg.Stride != 1 {
			t.Errorf("Nd range %x-%x is not whole sets of ten", rg.Lo, rg.Hi)
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if (int(rg.Hi)-int(rg.Lo)+1)%10 != 0 || rg.Stride != 1 {
			t.Errorf("Nd range %x-%x is not whole sets of ten", rg.Lo, rg.Hi)
		}
	}
}

func TestSpaceAndStrip(t *testing.T) {
	for _, r := range []rune{' ', '\t', ' ', ' ', ' ', '　', '\x1c', '\x1f'} {
		if !pystr.IsSpace(r) {
			t.Errorf("%q must be white space", r)
		}
	}
	if pystr.IsSpace('x') || pystr.IsSpace('​') {
		t.Error("x and the zero width space are no white space in Python")
	}
	if got := pystr.Strip("  mælk\x1f"); got != "mælk" {
		t.Errorf("Strip = %q", got)
	}
}

func TestFoldEq(t *testing.T) {
	cases := []struct {
		r, lit rune
		want   bool
	}{{'Ø', 'ø', true}, {'X', 'x', true}, {'İ', 'i', true}, {'ı', 'i', true}, {'ſ', 's', true},
		{'K', 'k', true}, {'a', 'b', false}, {'×', 'x', false}}
	for _, c := range cases {
		if got := pystr.FoldEq(c.r, c.lit); got != c.want {
			t.Errorf("FoldEq(%q, %q) = %v", c.r, c.lit, got)
		}
	}
}

func TestRepr(t *testing.T) {
	cases := map[string]string{
		"Pizza":  "'Pizza'",
		"":       "''",
		"it's":   `"it's"`,
		`a"b'c`:  `'a"b\'c'`,
		"a\nb\\": `'a\nb\\'`,
		"Rødløg": "'Rødløg'",
	}
	for in, want := range cases {
		if got := pystr.Repr(in); got != want {
			t.Errorf("Repr(%q) = %s, want %s", in, got, want)
		}
	}
}
