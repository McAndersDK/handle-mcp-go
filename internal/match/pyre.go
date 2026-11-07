package match

import (
	"strings"
	"unicode"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

// The regular expressions of match.py, written by hand. Go's regexp has ASCII \b \s \d \w, no
// lookahead, and folds case differently from Python's re.IGNORECASE; each function here
// says which Python pattern it stands for. They work on runes, because Python counts characters.

func digitsEnd(rs []rune, i int) int {
	for i < len(rs) && pystr.IsDigit(rs[i]) {
		i++
	}
	return i
}

func spacesEnd(rs []rune, i int) int {
	for i < len(rs) && pystr.IsSpace(rs[i]) {
		i++
	}
	return i
}

func isX(r rune) bool { return r == '×' || pystr.FoldEq(r, 'x') } // [x×], ignore case

// unitFold matches one letter of a unit, ignoring case. Python's IGNORECASE also lets "ſ", "ı"
// and "İ" in, and then str.lower() gives a text that is no unit: a KeyError in Python.
// Go does not match them. The Kelvin sign is "k" for both.
func unitFold(r, lit rune) bool {
	return r == lit || r == lit-'a'+'A' || (lit == 'k' && r == 0x212A)
}

// sizeMatch is one match of SIZE_RE:
// (?:(\d+)\s*[x×]\s*)?(\d+(?:[.,]\d+)?)\s*(g|gr|kg|...)\b
// The amount is wider in Go: it may also be a fraction (fraction.go). That is a Go-only addition.
type sizeMatch struct {
	end      int
	count    string // the "N x" before the amount; empty when there is none
	hasCount bool
	amount   model.Money // 1.5 for "1,5"
	bad      bool        // a fraction with denominator 0, or a number too large: no size
	unit     string      // "kg", as written
}

func sizeAt(rs []rune, i int) (sizeMatch, bool) {
	if j := digitsEnd(rs, i); j > i {
		k := spacesEnd(rs, j)
		if k < len(rs) && isX(rs[k]) {
			if m, ok := amountAt(rs, spacesEnd(rs, k+1)); ok {
				m.count, m.hasCount = string(rs[i:j]), true
				return m, true
			}
		}
	}
	return amountAt(rs, i)
}

func amountAt(rs []rune, i int) (sizeMatch, bool) {
	if i > 0 && rs[i-1] == '/' {
		return sizeMatch{}, false // "/2 dl" has no numerator: not an amount
	}
	amount, j, bad, ok := amountNumber(rs, i)
	if !ok {
		return sizeMatch{}, false
	}
	u := spacesEnd(rs, j)
	for _, unit := range units {
		end := u + len(unit.name)
		if end > len(rs) {
			continue
		}
		ok := true
		for n, lit := range unit.name {
			if !unitFold(rs[u+n], lit) {
				ok = false
				break
			}
		}
		// \b after a letter: the next character must not be a word character
		if ok && (end == len(rs) || !pystr.IsWord(rs[end])) {
			return sizeMatch{end: end, amount: amount, bad: bad, unit: string(rs[u:end])}, true
		}
	}
	return sizeMatch{}, false
}

func searchSize(rs []rune) (sizeMatch, bool) {
	for i := range rs {
		if m, ok := sizeAt(rs, i); ok {
			return m, true
		}
	}
	return sizeMatch{}, false
}

// subSize is SIZE_RE.sub(repl, text).
func subSize(text, repl string) string {
	rs := []rune(text)
	var b strings.Builder
	for i := 0; i < len(rs); {
		if m, ok := sizeAt(rs, i); ok {
			b.WriteString(repl)
			i = m.end
		} else {
			b.WriteRune(rs[i])
			i++
		}
	}
	return b.String()
}

// matchQty is QTY_RE.match: ^\s*(\d+)\s*[x×]\s+(?=\D). It returns the digits and where the
// match ends. The lookahead needs a character that is not a digit: when the white space
// ahead of a digit is longer than one, the match gives one back and ends inside it.
func matchQty(rs []rune) (string, int, bool) {
	i := spacesEnd(rs, 0)
	j := digitsEnd(rs, i)
	if j == i {
		return "", 0, false
	}
	k := spacesEnd(rs, j)
	if k >= len(rs) || !isX(rs[k]) {
		return "", 0, false
	}
	w := spacesEnd(rs, k+1)
	switch {
	case w == k+1:
		return "", 0, false
	case w < len(rs) && !pystr.IsDigit(rs[w]):
		return string(rs[i:j]), w, true
	case w-(k+1) >= 2:
		return string(rs[i:j]), w - 1, true
	}
	return "", 0, false
}

// flavourWords are FLAVOUR_RE: \s(?:m\.?|med|i|u\.|uden|smag)\s, in the order of the pattern.
var flavourWords = [][]rune{[]rune("m."), []rune("m"), []rune("med"), []rune("i"),
	[]rune("u."), []rune("uden"), []rune("smag")}

// findFlavour is FLAVOUR_RE.search: where the match starts and ends (after its last space).
func findFlavour(rs []rune) (int, int, bool) {
	for i := range rs {
		if !pystr.IsSpace(rs[i]) {
			continue
		}
		for _, w := range flavourWords {
			end := i + 1 + len(w)
			if end >= len(rs) || !pystr.IsSpace(rs[end]) {
				continue
			}
			ok := true
			for n, lit := range w {
				r := rs[i+1+n]
				if (lit == '.' && r != '.') || (lit != '.' && !pystr.FoldEq(r, lit)) {
					ok = false
					break
				}
			}
			if ok {
				return i, end + 1, true
			}
		}
	}
	return 0, 0, false
}

// splitFlavour is FLAVOUR_RE.split(text, maxsplit=1): the text before and after the first
// match. found is false when Python's result would have one part only.
func splitFlavour(text string) (before, after string, found bool) {
	rs := []rune(text)
	s, e, ok := findFlavour(rs)
	if !ok {
		return text, "", false
	}
	return string(rs[:s]), string(rs[e:]), true
}

// isBlendLetter is [a-zæøå] with IGNORECASE: also "İ", "ı", "ſ" and the Kelvin sign.
func isBlendLetter(r rune) bool {
	l := unicode.ToLower(r)
	return (l >= 'a' && l <= 'z') || l == 'æ' || l == 'ø' || l == 'å' || r == 'ı' || r == 'ſ'
}

// blendWords is BLEND_RE.findall: the word after each "50 %" in text. BLEND_RE is
// \d+(?:[.,]\d+)?\s*%\s*([a-zæøå]+) with IGNORECASE.
func blendWords(text string) []string {
	rs := []rune(text)
	var out []string
	for i := 0; i < len(rs); {
		j := digitsEnd(rs, i)
		if j == i {
			i++
			continue
		}
		if j+1 < len(rs) && (rs[j] == '.' || rs[j] == ',') && pystr.IsDigit(rs[j+1]) {
			j = digitsEnd(rs, j+1)
		}
		k := spacesEnd(rs, j)
		if k >= len(rs) || rs[k] != '%' {
			i++
			continue
		}
		w := spacesEnd(rs, k+1)
		e := w
		for e < len(rs) && isBlendLetter(rs[e]) {
			e++
		}
		if e == w {
			i++
			continue
		}
		out = append(out, string(rs[w:e]))
		i = e
	}
	return out
}

// abbreviations maps a whole name word to its full form. Go-only (ADR 0005): REMA 1000
// writes "HK." for "hakket". Only whole words change, never a word that contains them.
var abbreviations = map[string]string{"hk": "hakket"}

// words is _words: re.findall(r"[a-zæøå0-9]+", text.lower()), plus the abbreviations
// table. ASCII digits only.
func words(text string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			w := string(cur)
			if full, ok := abbreviations[w]; ok {
				w = full
			}
			out = append(out, w)
			cur = cur[:0]
		}
	}
	for _, r := range pystr.Lower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == 'æ' || r == 'ø' || r == 'å' {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// oekoWords are OEKO_RE: \b(?:øko|økologiske?|organic)\b, ignoring case. "økologiske?" is
// two spellings, the longer first.
var oekoWords = [][]rune{[]rune("øko"), []rune("økologiske"), []rune("økologisk"), []rune("organic")}

func hasOeko(text string) bool {
	rs := []rune(text)
	for i := range rs {
		if i > 0 && pystr.IsWord(rs[i-1]) {
			continue // \b needs a non-word character before the first letter
		}
		for _, w := range oekoWords {
			end := i + len(w)
			if end > len(rs) || (end < len(rs) && pystr.IsWord(rs[end])) {
				continue
			}
			ok := true
			for n, lit := range w {
				if !pystr.FoldEq(rs[i+n], lit) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// marked is _marked: a word of PREPARED starts or ends a word. The pattern is \bX|X\b for each
// X, with no IGNORECASE: the text is lower case already.
func marked(text string) bool {
	rs := []rune(text)
	for _, m := range preparedWords {
		mr := []rune(m)
		for i := 0; i+len(mr) <= len(rs); i++ {
			if string(rs[i:i+len(mr)]) != m {
				continue
			}
			startsWord := i == 0 || !pystr.IsWord(rs[i-1])
			endsWord := i+len(mr) == len(rs) || !pystr.IsWord(rs[i+len(mr)])
			if startsWord || endsWord {
				return true
			}
		}
	}
	return false
}
