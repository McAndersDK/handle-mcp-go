// Package pystr holds the few string rules where Python and Go differ, so the matcher
// behaves like the Python it was ported from (ADR 0005, golden files in tests/golden/match).
//
//   - len, slicing and endswith count characters, not bytes: use Len and runes.
//   - \s, \d, \w and \b are Unicode in Python's re (and ASCII in Go's regexp).
//   - str.lower() maps "İ" to "i" plus a combining dot; Go's ToLower gives plain "i".
package pystr

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Len is Python's len(str): characters, not bytes.
func Len(s string) int { return utf8.RuneCountInString(s) }

// IsSpace is Python's \s and str.isspace(): Unicode white space plus \x1c-\x1f.
func IsSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// IsDigit is Python's \d for str: any Unicode decimal digit (category Nd).
func IsDigit(r rune) bool { return unicode.IsDigit(r) }

// IsWord is Python's \w: letters, numbers and "_".
func IsWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// DigitValue is the value 0-9 of a Unicode decimal digit, e.g. 2 for the Arabic-Indic "٢".
// Every Nd range is made of whole sets of ten digits, each set starting at zero.
func DigitValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	if r < 0x80 || !unicode.IsDigit(r) {
		return 0, false
	}
	for _, rg := range unicode.Nd.R16 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10, true
		}
	}
	for _, rg := range unicode.Nd.R32 {
		if r >= rune(rg.Lo) && r <= rune(rg.Hi) {
			return int(r-rune(rg.Lo)) % 10, true
		}
	}
	return 0, false
}

// ASCIIDigits replaces every Unicode digit by 0-9, as Python's int() and Decimal() read them.
func ASCIIDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if v, ok := DigitValue(r); ok {
			b.WriteByte(byte('0' + v))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Lower is Python's str.lower(): like strings.ToLower, but "İ" becomes "i" + U+0307.
// (Python's final-sigma rule is left out: it only touches Greek.)
func Lower(s string) string {
	if !strings.ContainsRune(s, 0x130) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	for _, r := range s {
		if r == 0x130 {
			b.WriteString("i̇")
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Strip is Python's str.strip(): Unicode white space from both ends.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// FoldEq is Python's re.IGNORECASE for the lower-case letter lit: "I" and "İ" and "ı" are
// "i", "ſ" is "s", the Kelvin sign is "k".
func FoldEq(r, lit rune) bool {
	if r == lit || unicode.ToLower(r) == lit {
		return true
	}
	return (lit == 'i' && r == 'ı') || (lit == 's' && r == 'ſ')
}

// Repr is Python's repr() of a str, for messages like "Opskriften har ikke 'Pizza'".
func Repr(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case strconv.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(`\x` + hex(r, 2))
		case r < 0x10000:
			b.WriteString(`\u` + hex(r, 4))
		default:
			b.WriteString(`\U` + hex(r, 8))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func hex(r rune, width int) string {
	s := strconv.FormatInt(int64(r), 16)
	return strings.Repeat("0", width-len(s)) + s
}
