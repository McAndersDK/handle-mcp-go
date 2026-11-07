package livelist

// The list and the checker: the port of the helpers in upstream/tests/test_live_list.py (items_for,
// problems, check, _fits, _any). The Python regex used a Unicode \b; Go's \b is ASCII only, so the
// word rule is written out by hand in anyWord.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/McAndersDK/handle-mcp-go/internal/candidates"
	"github.com/McAndersDK/handle-mcp-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// listPath is the shared list, relative to this package. The Python file stays where it is.
const listPath = "../../tests/live_list.json"

// storeNames are the stores the list is priced in (the server's registry has the same three).
var storeNames = []string{"nemlig", "bilkatogo", "rema1000"}

// Entry is one line of the list. Unknown keys are an error (the Python KEYS set).
type Entry struct {
	Item   string       `json:"item"`
	Has    []string     `json:"has"`
	Not    []string     `json:"not"`
	Stores []string     `json:"stores"`
	MaxKr  *model.Money `json:"max_kr"`
	Why    string       `json:"why"`
}

type listFile struct {
	About string  `json:"about"`
	Items []Entry `json:"items"`
}

func loadList() ([]Entry, error) {
	raw, err := os.ReadFile(listPath)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f listFile
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	return f.Items, nil
}

// itemsFor are the entries priced in one store (no "stores" key: all of them).
func itemsFor(list []Entry, store string) []Entry {
	var out []Entry
	for _, e := range list {
		if e.Stores == nil || slices.Contains(e.Stores, store) {
			out = append(out, e)
		}
	}
	return out
}

// oekoMargin: øko only when it costs at most 10 % more. Money has no fraction multiply, so the
// test is min*110 < total*100.
const oekoNum, oekoDen = 110, 100

// problems is what is wrong with one candidate for one entry. Empty when it fits.
func problems(e Entry, c candidates.Candidate) []string {
	text := strings.ToLower(c.Name + " " + deref(c.Brand))
	var out []string
	for _, w := range e.Has {
		if !anyWord(w, text) {
			out = append(out, fmt.Sprintf("mangler '%s'", w))
		}
	}
	for _, w := range e.Not {
		if anyWord(w, text) {
			out = append(out, fmt.Sprintf("har '%s'", w))
		}
	}
	if e.MaxKr != nil && c.LineTotal.Cmp(*e.MaxKr) > 0 {
		out = append(out, fmt.Sprintf("%s kr. > %s kr.", c.LineTotal, *e.MaxKr))
	}
	return out
}

// check returns (fail, warn). Fail: no candidate fits. Warn: the suggestion is wrong or øko too
// dear. A nil line (or one with no product) is "ikke fundet".
func check(e Entry, line *compare.BasketLine) (fail, warn []string) {
	if line == nil || line.Product == nil {
		return []string{"ikke fundet"}, nil
	}
	var cands []candidates.Candidate
	for _, c := range line.Candidates {
		if !(c.Suggested && line.Guess) {
			cands = append(cands, c)
		}
	}
	if line.Guess {
		warn = []string{"gæt: " + line.Product.Name}
	} else if i := slices.IndexFunc(line.Candidates, func(c candidates.Candidate) bool { return c.Suggested }); i >= 0 {
		warn = problems(e, line.Candidates[i])
	}
	if !slices.ContainsFunc(cands, func(c candidates.Candidate) bool { return len(problems(e, c)) == 0 }) {
		return append([]string{fmt.Sprintf("ingen kandidat passer (%d vist)", len(cands))}, warn...), nil
	}
	if line.Oeko {
		var plain *model.Money
		for _, c := range line.Candidates {
			if c.Suggested || c.Oeko || !fits(e, c) {
				continue
			}
			if plain == nil || c.LineTotal.Cmp(*plain) < 0 {
				t := c.LineTotal
				plain = &t
			}
		}
		if plain != nil && plain.Mul(oekoNum).Cmp(line.LineTotal.Mul(oekoDen)) < 0 {
			warn = append(warn, fmt.Sprintf("øko %s kr. mod %s kr.", line.LineTotal, *plain))
		}
	}
	return nil, warn
}

// fits: an alternative counts for the øko rule only when it is the same kind of thing: not
// 'Citron peber' for citroner, not a tin ('Konserves') for cherrytomater.
func fits(e Entry, c candidates.Candidate) bool {
	text := strings.ToLower(c.Name + " " + deref(c.Brand) + " " + deref(c.Category))
	for _, w := range e.Has {
		if !anyWord(w, text) {
			return false
		}
	}
	for _, w := range e.Not {
		if anyWord(w, text) {
			return false
		}
	}
	return true
}

// anyWord: 'a|b' means a or b. A word must start or end a name word, with a Danish plural ending:
// 'kylling' hits 'Kyllingebryst', 'olie' hits 'krydderolie', 'tomat' hits 'Cherrytomater'. A word
// deep inside another does not: 'ost' is not 'Leverpostej', 'ris' is not 'friske'. This is the
// Python pattern  \bW|W(?:e|er|r|s)?\b  with Unicode word characters.
func anyWord(word, text string) bool {
	for _, w := range strings.Split(strings.ToLower(word), "|") {
		for from := 0; from <= len(text); {
			i := strings.Index(text[from:], w)
			if i < 0 {
				break
			}
			i += from
			end := i + len(w)
			if boundary(text, i) {
				return true
			}
			for _, suffix := range []string{"e", "er", "r", "s", ""} {
				if strings.HasPrefix(text[end:], suffix) && boundary(text, end+len(suffix)) {
					return true
				}
			}
			_, n := utf8.DecodeRuneInString(text[i:])
			from = i + max(n, 1) // overlapping hits count
		}
	}
	return false
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// boundary is Python's \b at byte offset p: a word character on exactly one side.
func boundary(text string, p int) bool {
	before, after := false, false
	if p > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:p])
		before = isWord(r)
	}
	if p < len(text) {
		r, _ := utf8.DecodeRuneInString(text[p:])
		after = isWord(r)
	}
	return before != after
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
