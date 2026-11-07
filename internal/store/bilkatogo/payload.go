package bilkatogo

// payload.js is `__NUXT_JSONP__("/path", {data: ...})`: a JavaScript object literal, not JSON. Keys
// have no quotes, undefined is `void 0`, and a slash in a string is written /. This reader
// understands that small subset and nothing else. It keeps the keys in source order, because the
// recipe code reads a page in page order (Python's dict kept it; a Go map does not).
//
// Anything else (a variable name, a function, a stray character, a page nested too deep) is the
// format-change error. The reader never panics and never guesses a value: Python turned a bare
// name into the string of its own name, which would be a wrong value, not a missing one.

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

const formatChanged = "BilkaToGo-siden har ændret format."

// maxDepth stops a hostile or broken page before the recursion gets deep. The real pages are
// 14 levels deep (TestEveryFixtureIsReadAndFarFromTheDepthLimit).
const maxDepth = 200

// pobj is a JavaScript object in source order. A value is *pobj, []any, string, json.Number, bool
// or nil. A key that comes twice keeps its first place and its last value, as in Python's dict.
type pobj struct {
	keys []string
	vals map[string]any
}

func (o *pobj) get(key string) any {
	if o == nil {
		return nil
	}
	return o.vals[key]
}

func (o *pobj) set(key string, v any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, there := o.vals[key]; !there {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func errFormat() error { return store.Errorf("%s", formatChanged) }

type jsReader struct {
	s     string
	i     int
	depth int
}

// parsePayload reads the object a payload.js holds: from the first "{" to its match. After it only
// whitespace, ")" and ";" may follow.
func parsePayload(src string) (*pobj, error) {
	src = strings.ToValidUTF8(src, "�")
	start := strings.IndexByte(src, '{')
	if start < 0 {
		return nil, errFormat()
	}
	r := &jsReader{s: src, i: start}
	v, ok := r.value()
	if !ok {
		return nil, errFormat()
	}
	for r.i < len(r.s) {
		if c := r.s[r.i]; !isBlank(c) && c != ')' && c != ';' {
			return nil, errFormat()
		}
		r.i++
	}
	o, isObj := v.(*pobj)
	if !isObj {
		return nil, errFormat()
	}
	return o, nil
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (r *jsReader) skip() {
	for r.i < len(r.s) && isBlank(r.s[r.i]) {
		r.i++
	}
}

func (r *jsReader) eat(c byte) bool {
	r.skip()
	if r.i < len(r.s) && r.s[r.i] == c {
		r.i++
		return true
	}
	return false
}

// value reads one value. false means the text is not in the subset.
func (r *jsReader) value() (any, bool) {
	r.skip()
	if r.i >= len(r.s) {
		return nil, false
	}
	switch c := r.s[r.i]; {
	case c == '{':
		return r.object()
	case c == '[':
		return r.array()
	case c == '"':
		return r.str()
	case c == '-' || c >= '0' && c <= '9':
		return r.number()
	case isIdentStart(r.peekRune()):
		switch word := r.ident(); word {
		case "true":
			return true, true
		case "false":
			return false, true
		case "null", "undefined":
			return nil, true
		case "void": // void 0
			r.skip()
			if r.i < len(r.s) && r.s[r.i] == '0' {
				r.i++
				return nil, true
			}
		}
	}
	return nil, false // a variable, a function call or anything else
}

func (r *jsReader) enter() bool {
	r.depth++
	return r.depth <= maxDepth
}

func (r *jsReader) object() (any, bool) {
	if !r.enter() {
		return nil, false
	}
	defer func() { r.depth-- }()
	r.i++ // {
	o := &pobj{}
	if r.eat('}') {
		return o, true
	}
	for {
		r.skip()
		key, ok := r.key()
		if !ok || !r.eat(':') {
			return nil, false
		}
		v, ok := r.value()
		if !ok {
			return nil, false
		}
		o.set(key, v)
		if r.eat('}') {
			return o, true
		}
		if !r.eat(',') {
			return nil, false
		}
	}
}

func (r *jsReader) array() (any, bool) {
	if !r.enter() {
		return nil, false
	}
	defer func() { r.depth-- }()
	r.i++ // [
	list := []any{}
	if r.eat(']') {
		return list, true
	}
	for {
		v, ok := r.value()
		if !ok {
			return nil, false
		}
		list = append(list, v)
		if r.eat(']') {
			return list, true
		}
		if !r.eat(',') {
			return nil, false
		}
	}
}

// key is a quoted string, a bare name or a number.
func (r *jsReader) key() (string, bool) {
	if r.i >= len(r.s) {
		return "", false
	}
	switch c := r.s[r.i]; {
	case c == '"':
		v, ok := r.str()
		s, _ := v.(string)
		return s, ok
	case c >= '0' && c <= '9':
		v, ok := r.number()
		n, _ := v.(json.Number)
		return string(n), ok
	case isIdentStart(r.peekRune()):
		return r.ident(), true
	}
	return "", false
}

func (r *jsReader) peekRune() rune {
	c, _ := utf8.DecodeRuneInString(r.s[r.i:])
	return c
}

func isIdentStart(c rune) bool { return c == '_' || c == '$' || unicode.IsLetter(c) }
func isIdentPart(c rune) bool  { return isIdentStart(c) || unicode.IsDigit(c) }

func (r *jsReader) ident() string {
	start := r.i
	for r.i < len(r.s) {
		c, n := utf8.DecodeRuneInString(r.s[r.i:])
		if !isIdentPart(c) {
			break
		}
		r.i += n
	}
	return r.s[start:r.i]
}
