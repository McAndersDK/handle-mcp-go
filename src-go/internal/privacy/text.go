package privacy

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Go's RE2 has no lookaround, and \b, \w, \d are ASCII. Python's are Unicode, so
// "Ærøvej 12" and "blåbær@mælk.dk" must match here as they do there. Word characters
// are written as [\p{L}\p{N}_]; the boundary and digit lookarounds are checked on runes
// by hand. [0-9], [A-Za-z] stay ASCII, as in Python.

const (
	streetEnd = "vej|gade|allé|alle|vænge|vænget|stræde|plads|torv|boulevard|parken|park|" +
		"haven|have|bakken|toften|engen|marken|stien|sti|høj|lund|ager|" +
		"vejen|gaden|alléen|alleen|strædet|pladsen|torvet|boulevarden|højen|lunden|ageren"
	streetWord = "Vej|Gade|Allé|Alle|Boulevard|Plads|Torv|Stræde|Vænge"
	word       = `\p{L}\p{N}_`
)

var (
	reEmail = regexp.MustCompile(`[` + word + `.+-]+@[` + word + `-]+(?:\.[` + word + `-]+)+`)
	// The street match stops before the closing \b, which is checked by hand.
	reStreet = regexp.MustCompile(`\A(?:[A-ZÆØÅ][` + word + `.'-]*(?:` + streetEnd + `)|` +
		streetWord + `)\.? +[0-9]{1,4}[A-Za-z]?`)
	reCity     = regexp.MustCompile(`\A[0-9]{4} +[A-ZÆØÅ][a-zæøå]+`)
	reCityTail = regexp.MustCompile(`\A +[A-ZÆØÅ]`)
	rePhonePre = regexp.MustCompile(`\A(?:\+|00)45 ?(?:[0-9]{2} ?){3}[0-9]{2}`)
	rePhone    = regexp.MustCompile(`\A(?:[0-9]{2} ?){3}[0-9]{2}`)
)

// ScrubText replaces emails, Danish phone numbers and addresses in free text (deep).
func ScrubText(value any) any {
	switch v := value.(type) {
	case string:
		return scrubString(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = ScrubText(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = ScrubText(item)
		}
		return out
	}
	return value
}

// scrubString applies the four patterns in order, each on the result of the last.
func scrubString(s string) string {
	s = reEmail.ReplaceAllLiteralString(s, Redacted)
	s = sub(s, matchPhone)
	s = sub(s, matchStreet)
	return sub(s, matchCity)
}

// sub replaces every non-overlapping match, scanning left to right like re.sub.
// match returns the end of a match that starts at byte i, or -1.
func sub(s string, match func(s string, i int) int) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if end := match(s, i); end > i {
			b.WriteString(s[last:i])
			b.WriteString(Redacted)
			i, last = end, end
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	b.WriteString(s[last:])
	return b.String()
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// wordBefore: the rune before byte i is a word character (false at the start).
func wordBefore(s string, i int) bool {
	r, n := utf8.DecodeLastRuneInString(s[:i])
	return n > 0 && isWord(r)
}

// wordAt: the rune at byte i is a word character (false at the end).
func wordAt(s string, i int) bool {
	r, n := utf8.DecodeRuneInString(s[i:])
	return n > 0 && isWord(r)
}

func digitBefore(s string, i int) bool { return i > 0 && s[i-1] >= '0' && s[i-1] <= '9' }

func digitAt(s string, i int) bool { return i < len(s) && s[i] >= '0' && s[i] <= '9' }

// matchPhone is (?<![0-9])(?:(?:\+|00)45 ?)?(?:[0-9]{2} ?){3}[0-9]{2}(?![0-9]).
// With the prefix first; if a digit follows, the no-prefix form is tried, as backtracking would.
func matchPhone(s string, i int) int {
	if digitBefore(s, i) {
		return -1
	}
	for _, re := range []*regexp.Regexp{rePhonePre, rePhone} {
		if loc := re.FindStringIndex(s[i:]); loc != nil && !digitAt(s, i+loc[1]) {
			return i + loc[1]
		}
	}
	return -1
}

// matchStreet is \b(?:...)\.? +[0-9]{1,4}[A-Za-z]?\b. Backtracking never helps once the
// greedy match is followed by a word character, so one check is enough.
func matchStreet(s string, i int) int {
	if wordBefore(s, i) {
		return -1
	}
	loc := reStreet.FindStringIndex(s[i:])
	if loc == nil || wordAt(s, i+loc[1]) {
		return -1
	}
	return i + loc[1]
}

// matchCity is \b[0-9]{4} +[A-ZÆØÅ][a-zæøå]+(?: +[A-ZÆØÅ]\b)?.
func matchCity(s string, i int) int {
	if wordBefore(s, i) {
		return -1
	}
	loc := reCity.FindStringIndex(s[i:])
	if loc == nil {
		return -1
	}
	end := i + loc[1]
	if tail := reCityTail.FindStringIndex(s[end:]); tail != nil && !wordAt(s, end+tail[1]) {
		end += tail[1]
	}
	return end
}
