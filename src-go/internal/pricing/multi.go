package pricing

import "github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"

// findMulti is MULTI_RE.search: (\d+)\s*for\s*(\d+(?:[.,]\d+)?), ignoring case ("4 for 50 kr"). It
// returns the two numbers as written. Python's \d, \s and IGNORECASE are Unicode; Go's regexp
// is not, so this is written by hand (see match/pyre.go).
func findMulti(text string) (size, total string, ok bool) {
	rs := []rune(text)
	for i := range rs {
		j := digitsEnd(rs, i)
		if j == i {
			continue
		}
		k := spacesEnd(rs, j)
		if k+3 > len(rs) || !pystr.FoldEq(rs[k], 'f') || !pystr.FoldEq(rs[k+1], 'o') || !pystr.FoldEq(rs[k+2], 'r') {
			continue
		}
		a := spacesEnd(rs, k+3)
		b := digitsEnd(rs, a)
		if b == a {
			continue
		}
		if b+1 < len(rs) && (rs[b] == '.' || rs[b] == ',') && pystr.IsDigit(rs[b+1]) {
			b = digitsEnd(rs, b+1)
		}
		return string(rs[i:j]), string(rs[a:b]), true
	}
	return "", "", false
}

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
