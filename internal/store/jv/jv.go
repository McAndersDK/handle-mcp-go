// Package jv reads the JSON the stores answer with, the way the Python adapters read it.
//
// An answer is decoded into plain values (maps, slices, strings, json.Number, bool, nil), so every
// `x or y`, `.get(k, d)` and `int(x)` keeps its meaning. Money never goes through float64.
//
// A shape we do not understand is raised with Bad() and turned into one clear StoreError at the
// edge of each public method by Recover. A StoreError raised deep inside a parser travels the same
// way with Raise(). Nothing else is recovered. Shared by the nemlig and bilkatogo packages.
package jv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// Obj is a decoded JSON object.
type Obj = map[string]any

// ShapeError is an answer we cannot read.
type ShapeError struct{ what string }

// Failure is a StoreError raised deep inside a parser.
type Failure struct{ err error }

// Bad panics with a ShapeError.
func Bad(format string, args ...any) { panic(ShapeError{fmt.Sprintf(format, args...)}) }

// Raise panics with a StoreError that Recover hands back as it is.
func Raise(err error) { panic(Failure{err}) }

// Recover is the body of a store's guard: call it with the value of recover(). A ShapeError becomes
// a StoreError with msg (only the text of the problem is logged, with who, never a value from the
// answer); a raised StoreError comes out as it is; anything else panics again.
func Recover(r any, msg, who string) error {
	switch x := r.(type) {
	case Failure:
		return x.err
	case ShapeError:
		slog.Warn(who+": unexpected answer shape", "what", x.what)
		return store.Errorf("%s", msg)
	}
	panic(r)
}

// DecodeJSON reads one JSON value; trailing text is an error, as in Python.
func DecodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("extra data")
	}
	return v, nil
}

// Truthy is Python's bool(): null, false, 0, "", [] and {} are false.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		m, err := model.ParseMoney(string(x))
		return err != nil || !m.IsZero()
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// ObjOrEmpty is `v or {}`. A truthy value that is not an object cannot be read: Bad().
func ObjOrEmpty(v any) Obj {
	if !Truthy(v) {
		return Obj{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		Bad("expected an object")
	}
	return m
}

// ListOrEmpty is `v or []`.
func ListOrEmpty(v any) []any {
	if !Truthy(v) {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		Bad("expected a list")
	}
	return l
}

// AsObj is an element that must be an object.
func AsObj(v any) Obj {
	m, ok := v.(map[string]any)
	if !ok {
		Bad("expected an object")
	}
	return m
}

// Field is m[key] for a required key: a missing key is KeyError in Python.
func Field(m Obj, key string) any {
	v, ok := m[key]
	if !ok {
		Bad("missing key %s", key)
	}
	return v
}

// WithDefault is m.get(key, def): the default only for a missing key, not for null.
func WithDefault(m Obj, key string, def any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func Has(m Obj, key string) bool { _, ok := m[key]; return ok }

var intText = regexp.MustCompile(`^-?[0-9]+$`)

// PyStr is Python's str() of a decoded JSON value.
func PyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	case json.Number:
		if intText.MatchString(string(x)) {
			s := strings.TrimLeft(string(x), "-")
			s = strings.TrimLeft(s, "0")
			if s == "" {
				return "0"
			}
			if strings.HasPrefix(string(x), "-") {
				return "-" + s
			}
			return s
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil {
			return string(x)
		}
		return PyFloat(f)
	}
	return PyRepr(v)
}

// PyRepr is Python's repr() of a decoded JSON value (used for str(dict) and str(list)).
func PyRepr(v any) string {
	switch x := v.(type) {
	case string:
		return pystr.Repr(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = PyRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		// Python keeps the order of the JSON text; Go maps do not, so keys are sorted.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = pystr.Repr(k) + ": " + PyRepr(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return PyStr(v)
}

// PyFloat is Python's repr(float).
func PyFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±xx, shortest digits
	mant, expText, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expText)
	neg := strings.HasPrefix(mant, "-")
	digits := strings.Replace(strings.TrimPrefix(mant, "-"), ".", "", 1)
	var s string
	switch {
	case exp >= 16 || exp < -4:
		s = digits[:1]
		if len(digits) > 1 {
			s += "." + digits[1:]
		}
		sign := "+"
		if exp < 0 {
			sign, exp = "-", -exp
		}
		s += fmt.Sprintf("e%s%02d", sign, exp)
	case exp >= 0:
		for len(digits) < exp+1 {
			digits += "0"
		}
		s = digits[:exp+1] + "." + digits[exp+1:]
		if strings.HasSuffix(s, ".") {
			s += "0"
		}
	default:
		s = "0." + strings.Repeat("0", -exp-1) + digits
	}
	if neg {
		s = "-" + s
	}
	return s
}

var pyIntText = regexp.MustCompile(`^[+-]?[0-9]+(_[0-9]+)*$`)

// ToInt is Python's int(): numbers truncate, strings must be whole numbers.
func ToInt(v any) int {
	switch x := v.(type) {
	case bool:
		if x {
			return 1
		}
		return 0
	case json.Number:
		if n, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return int(n)
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) >= 1<<62 {
			Bad("not an integer")
		}
		return int(f)
	case string:
		s := pystr.ASCIIDigits(pystr.Strip(x))
		if !pyIntText.MatchString(s) {
			Bad("not an integer")
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
		if err != nil {
			Bad("integer out of range")
		}
		return int(n)
	}
	Bad("not an integer")
	return 0
}

// ToIntOrZero is int(v or 0).
func ToIntOrZero(v any) int {
	if !Truthy(v) {
		return 0
	}
	return ToInt(v)
}

// NumVal is a number as Python compares it: a bool is 0 or 1.
func NumVal(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// IsNum is `v == n` for a JSON value.
func IsNum(v any, n float64) bool {
	f, ok := NumVal(v)
	return ok && f == n
}

// IsPyInt is isinstance(v, int): a JSON number written without a fraction or exponent, or a bool.
func IsPyInt(v any) bool {
	switch x := v.(type) {
	case bool:
		return true
	case json.Number:
		return intText.MatchString(string(x))
	}
	return false
}

// OptStr is `v or None` for a text.
func OptStr(v any) *string {
	if !Truthy(v) {
		return nil
	}
	s := PyStr(v)
	return &s
}

// TextOf is a string that must be a string when it is there ("" for nothing).
func TextOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return PyStr(v)
}

// MustText is a truthy value that has to be text (Python would fail on anything else).
func MustText(v any) string {
	s, ok := v.(string)
	if !ok {
		Bad("expected a text")
	}
	return s
}

func StrPtr(s string) *string { return &s }

// ToInt64 is int() for amounts in øre.
func ToInt64(v any) int64 {
	if n, ok := v.(json.Number); ok {
		if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
			return i
		}
	}
	return int64(ToInt(v))
}
