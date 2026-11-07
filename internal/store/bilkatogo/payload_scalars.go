package bilkatogo

// The scalars of the payload.js reader: numbers and strings (see payload.go).

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf16"
)

// number is -?digits(.digits)?([eE][+-]?digits)?, and may not run into a name ("1e5x").
func (r *jsReader) number() (any, bool) {
	start := r.i
	if r.s[r.i] == '-' {
		r.i++
	}
	if !r.digits() {
		return nil, false
	}
	if r.i < len(r.s) && r.s[r.i] == '.' {
		r.i++
		if !r.digits() {
			return nil, false
		}
	}
	if r.i < len(r.s) && (r.s[r.i] == 'e' || r.s[r.i] == 'E') {
		r.i++
		if r.i < len(r.s) && (r.s[r.i] == '+' || r.s[r.i] == '-') {
			r.i++
		}
		if !r.digits() {
			return nil, false
		}
	}
	if r.i < len(r.s) && isIdentPart(r.peekRune()) {
		return nil, false
	}
	return json.Number(r.s[start:r.i]), true
}

func (r *jsReader) digits() bool {
	start := r.i
	for r.i < len(r.s) && r.s[r.i] >= '0' && r.s[r.i] <= '9' {
		r.i++
	}
	return r.i > start
}

// str reads a double-quoted string: the JSON escapes, \x and \'. A raw control character is refused,
// as json.loads did.
func (r *jsReader) str() (any, bool) {
	r.i++ // opening quote
	var b strings.Builder
	for r.i < len(r.s) {
		c := r.s[r.i]
		switch {
		case c == '"':
			r.i++
			return b.String(), true
		case c < 0x20:
			return nil, false
		case c != '\\':
			b.WriteByte(c)
			r.i++
			continue
		}
		if r.i+1 >= len(r.s) {
			return nil, false
		}
		esc := r.s[r.i+1]
		r.i += 2
		switch esc {
		case '"', '\\', '/', '\'':
			b.WriteByte(esc)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x':
			n, ok := r.hex(2)
			if !ok {
				return nil, false
			}
			b.WriteRune(rune(n))
		case 'u':
			n, ok := r.hex(4)
			if !ok {
				return nil, false
			}
			b.WriteRune(r.pair(rune(n)))
		default:
			return nil, false
		}
	}
	return nil, false // no closing quote
}

// pair joins a high surrogate with the \uXXXX that follows it. A lone half becomes U+FFFD.
func (r *jsReader) pair(first rune) rune {
	if !utf16.IsSurrogate(first) {
		return first
	}
	if strings.HasPrefix(r.s[r.i:], `\u`) {
		save := r.i
		r.i += 2
		if n, ok := r.hex(4); ok {
			if joined := utf16.DecodeRune(first, rune(n)); joined != unicode.ReplacementChar {
				return joined
			}
		}
		r.i = save
	}
	return unicode.ReplacementChar
}

func (r *jsReader) hex(width int) (int, bool) {
	if r.i+width > len(r.s) {
		return 0, false
	}
	n := 0
	for _, c := range []byte(r.s[r.i : r.i+width]) {
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | int(c-'A'+10)
		default:
			return 0, false
		}
	}
	r.i += width
	return n, true
}
