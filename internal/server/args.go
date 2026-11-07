package server

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The argument decoder (ADR 0005, Arguments). The schema in tools.json says what each argument is.
// Every error is Danish and names the argument, never its value.

type kind int

const (
	kString kind = iota
	kInt
	kList                    // of strings
	kIntMap                  // string -> integer, in the order given
	kStrMap                  // string -> string, in the order given
	satur    = 1_000_000_000 // a longer number is clamped by the tool, or refused by its range check
	spaceSet = " \t\n\r\f\v"
)

// param is one argument of a tool.
type param struct {
	name     string
	kind     kind
	enum     []string
	required bool
	nullable bool // the schema allows null
	def      any  // used when the argument is absent: nil for a nullable one
}

// IntPair and StrPair are map entries in the order the caller wrote them.
type IntPair struct {
	Key string
	N   int
}

// StrPair is one entry of a string map.
type StrPair struct{ Key, Value string }

// Args are the decoded arguments of one call, by name.
type Args map[string]any

// Str is a string argument ("" when absent).
func (a Args) Str(name string) string { s, _ := a[name].(string); return s }

// OptStr is a nullable string argument.
func (a Args) OptStr(name string) *string {
	if s, ok := a[name].(string); ok {
		return &s
	}
	return nil
}

// Int is an integer argument (0 when absent).
func (a Args) Int(name string) int { n, _ := a[name].(int); return n }

// OptInt is a nullable integer argument.
func (a Args) OptInt(name string) *int {
	if n, ok := a[name].(int); ok {
		return &n
	}
	return nil
}

// List is a list of strings (nil for null or absent).
func (a Args) List(name string) []string { l, _ := a[name].([]string); return l }

// IntPairs is an integer map in call order.
func (a Args) IntPairs(name string) []IntPair { l, _ := a[name].([]IntPair); return l }

// StrPairs is a string map in call order.
func (a Args) StrPairs(name string) []StrPair { l, _ := a[name].([]StrPair); return l }

func bad(format string, a ...any) error { return toolErrorf(format, a...) }

// decodeArgs reads the arguments object of a tools/call request.
func decodeArgs(params []param, raw json.RawMessage) (Args, error) {
	in := &object{vals: map[string]any{}}
	if t := bytes.TrimSpace(raw); len(t) > 0 && string(t) != "null" {
		doc, err := parseDoc(t)
		obj, ok := doc.(*object)
		if err != nil || !ok {
			return nil, bad("Argumenterne skal være et JSON-objekt.")
		}
		in = obj
	}
	out := Args{}
	for _, p := range params { // unknown keys are ignored
		v, present := in.vals[p.name]
		if !present {
			if p.required {
				return nil, bad("Argumentet '%s' mangler.", p.name)
			}
			out[p.name] = p.def
			continue
		}
		val, err := p.convert(v)
		if err != nil {
			return nil, err
		}
		out[p.name] = val
	}
	return out, nil
}

func (p param) convert(v any) (any, error) {
	if s, isString := v.(string); isString && p.kind != kString {
		// Clients send lists and objects as JSON text. Like the Python SDK, a parsed array, object
		// or null replaces the string; any other result (a number, a word) leaves it as it was.
		if doc, err := parseDoc([]byte(s)); err == nil {
			switch doc.(type) {
			case []any, *object, nil:
				v = doc
			}
		}
	}
	if v == nil {
		if p.nullable {
			return nil, nil
		}
		return nil, bad("Argumentet '%s' må ikke være null.", p.name)
	}
	switch p.kind {
	case kString:
		return p.text(p.name, v)
	case kInt:
		return p.integer(p.name, v)
	case kList:
		list, ok := v.([]any)
		if !ok {
			return nil, bad("Argumentet '%s' skal være en liste.", p.name)
		}
		out := make([]string, len(list))
		for i, e := range list {
			s, ok := e.(string)
			if !ok {
				return nil, bad("Argumentet '%s.%d' skal være tekst.", p.name, i)
			}
			out[i] = s
		}
		return out, nil
	}
	obj, ok := v.(*object)
	if !ok {
		return nil, bad("Argumentet '%s' skal være et objekt.", p.name)
	}
	if p.kind == kIntMap {
		out := make([]IntPair, 0, len(obj.keys))
		for _, k := range obj.keys {
			n, err := p.integer(p.name+"."+k, obj.vals[k])
			if err != nil {
				return nil, err
			}
			out = append(out, IntPair{k, n.(int)})
		}
		return out, nil
	}
	out := make([]StrPair, 0, len(obj.keys))
	for _, k := range obj.keys {
		s, ok := obj.vals[k].(string)
		if !ok {
			return nil, bad("Argumentet '%s.%s' skal være tekst.", p.name, k)
		}
		out = append(out, StrPair{k, s})
	}
	return out, nil
}

func (p param) text(path string, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, bad("Argumentet '%s' skal være tekst.", path)
	}
	if len(p.enum) > 0 && !slices.Contains(p.enum, s) {
		return nil, bad("Argumentet '%s' skal være en af: %s.", path, strings.Join(p.enum, ", "))
	}
	return s, nil
}

func (p param) integer(path string, v any) (any, error) {
	var n int
	var ok bool
	switch x := v.(type) {
	case json.Number:
		n, ok = numberInt(string(x))
	case string:
		n, ok = stringInt(x)
	}
	if !ok {
		return nil, bad("Argumentet '%s' skal være et heltal.", path)
	}
	return n, nil
}

// numberInt accepts the JSON numbers 2 and 2.0 (and -2): a clear error beats a wrong quantity, so
// 2.5, 2e0 and the like are refused. A long number is clamped.
func numberInt(s string) (int, bool) {
	neg := strings.HasPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if hasFrac && (frac == "" || strings.Trim(frac, "0") != "") || !asciiDigits(whole) {
		return 0, false
	}
	n := clampDigits(whole)
	if neg {
		n = -n
	}
	return n, true
}

// stringInt accepts "2" and " 2 ": ASCII digits, surrounding whitespace, no leading zero except
// "0" itself. "2_0", "+2", "02", "2.0", "-1", "true" and non-ASCII digits are refused.
func stringInt(s string) (int, bool) {
	t := strings.Trim(s, spaceSet)
	if !asciiDigits(t) || (len(t) > 1 && t[0] == '0') {
		return 0, false
	}
	return clampDigits(t), true
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func clampDigits(digits string) int {
	if len(digits) > 9 {
		return satur
	}
	n, _ := strconv.Atoi(digits)
	return n
}
