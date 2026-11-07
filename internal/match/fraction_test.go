package match

import (
	"slices"
	"testing"
)

func TestFractions(t *testing.T) {
	cases := []struct {
		text  string
		base  string
		want  string // "" for no size
		words []string
	}{
		{"1/2 dl olie", "ml", "50", []string{"olie"}},
		{"2/3 dl mel", "ml", "66.67", []string{"mel"}},
		{"3/4 l mælk", "ml", "750", []string{"mælk"}},
		{"½ dl kogevand", "ml", "50", []string{"kogevand"}},
		{"⅓ l vand", "ml", "333.33", []string{"vand"}},
		{"⅔ dl", "ml", "66.67", nil},
		{"¼ kg smør", "g", "250", []string{"smør"}},
		{"¾ l", "ml", "750", nil},
		{"⅕ l", "ml", "200", nil},
		{"⅛ kg", "g", "125", nil},
		{"1½ dl", "ml", "150", nil},
		{"1 ½ dl", "ml", "150", nil},
		{"1 1/2 dl", "ml", "150", nil},
		{"2 3/4 l mælk", "ml", "2750", []string{"mælk"}},
		{"1,5 kg", "g", "1500", nil},
		{"1.5 kg", "g", "1500", nil},
		{"2 x 0,5 l", "ml", "1000", nil},
		{"2 x 1/2 l", "ml", "1000", nil},
		{"3 x ½ l", "ml", "1500", nil},
		{"12/24 stk", "stk", "0.5", nil},
		{"1/0 dl", "", "", nil},
		{"1/0 dl mel 2 kg", "", "", nil},
		{"/2 dl", "", "", nil},
		{"1/ dl", "", "", nil},
		{"2/3 mel", "", "", nil},
		{"2/3", "", "", nil},
	}
	for _, c := range cases {
		got := ParseSize(c.text)
		if c.want == "" {
			if got != nil {
				t.Errorf("ParseSize(%q) = %v, want none", c.text, got)
			}
		} else if got == nil || got.Base != c.base || got.Amount.String() != mustMoney(t, c.want).String() {
			t.Errorf("ParseSize(%q) = %v, want %s %s", c.text, got, c.base, c.want)
		}
		if c.words != nil {
			if _, ws, _ := ParseItem(c.text); !slices.Equal(ws, c.words) {
				t.Errorf("ParseItem(%q) words %q, want %q", c.text, ws, c.words)
			}
		}
	}
}

func TestFractionLeavesNoWords(t *testing.T) {
	for _, text := range []string{"1/2 dl", "½ dl", "1½ dl", "1 1/2 dl", "2 x 1/2 l"} {
		if _, ws, _ := ParseItem(text); len(ws) != 0 {
			t.Errorf("ParseItem(%q) words %q, want none", text, ws)
		}
	}
	if q := Query("1/2 dl olie"); q != "olie" {
		t.Errorf("Query = %q", q)
	}
}
