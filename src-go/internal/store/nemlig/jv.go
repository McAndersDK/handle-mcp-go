package nemlig

// Readers for nemlig's JSON. An answer is decoded into plain values (maps, slices, strings,
// json.Number, bool, nil) and read the way the Python adapter read it, so every `x or y`,
// `.get(k, d)` and `int(x)` keeps its meaning. Money never goes through float64.
//
// A shape we do not understand is raised with bad() and turned into one clear StoreError at the
// edge of each public method by guard(). Nothing else is recovered.

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

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

type obj = map[string]any

type shapeError struct{ what string }

func bad(format string, args ...any) { panic(shapeError{fmt.Sprintf(format, args...)}) }

// guard turns a shapeError into a StoreError. Use it as "defer guard(&err)". Only the text of
// the shape problem is logged, never a value from the answer.
func guard(err *error) {
	if r := recover(); r != nil {
		*err = shapeTo(r, "nemlig.com svarede i et uventet format. API'et kan have ændret sig.")
	}
}

// guardAs is guard with a message that names what changed.
func guardAs(err *error, msg string) {
	if r := recover(); r != nil {
		*err = shapeTo(r, msg)
	}
}

func shapeTo(r any, msg string) error {
	se, ok := r.(shapeError)
	if !ok {
		panic(r)
	}
	slog.Warn("nemlig: unexpected answer shape", "what", se.what)
	return store.Errorf("%s", msg)
}

// decodeJSON reads one JSON value; trailing text is an error, as in Python.
func decodeJSON(data []byte) (any, error) {
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

// truthy is Python's bool(): null, false, 0, "", [] and {} are false.
func truthy(v any) bool {
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

// objOrEmpty is `v or {}`. A truthy value that is not an object cannot be read: bad().
func objOrEmpty(v any) obj {
	if !truthy(v) {
		return obj{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		bad("expected an object")
	}
	return m
}

// listOrEmpty is `v or []`.
func listOrEmpty(v any) []any {
	if !truthy(v) {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		bad("expected a list")
	}
	return l
}

// asObj is an element that must be an object.
func asObj(v any) obj {
	m, ok := v.(map[string]any)
	if !ok {
		bad("expected an object")
	}
	return m
}

// field is m[key] for a required key: a missing key is KeyError in Python.
func field(m obj, key string) any {
	v, ok := m[key]
	if !ok {
		bad("missing key %s", key)
	}
	return v
}

// withDefault is m.get(key, def): the default only for a missing key, not for null.
func withDefault(m obj, key string, def any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

var intText = regexp.MustCompile(`^-?[0-9]+$`)

// pyStr is Python's str() of a decoded JSON value.
func pyStr(v any) string {
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
		return pyFloat(f)
	}
	return pyRepr(v)
}

// pyRepr is Python's repr() of a decoded JSON value (used for str(dict) and str(list)).
func pyRepr(v any) string {
	switch x := v.(type) {
	case string:
		return pystr.Repr(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = pyRepr(e)
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
			parts[i] = pystr.Repr(k) + ": " + pyRepr(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return pyStr(v)
}

// pyFloat is Python's repr(float).
func pyFloat(f float64) string {
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

// toInt is Python's int(): numbers truncate, strings must be whole numbers.
func toInt(v any) int {
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
			bad("not an integer")
		}
		return int(f)
	case string:
		s := pystr.ASCIIDigits(pystr.Strip(x))
		if !pyIntText.MatchString(s) {
			bad("not an integer")
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
		if err != nil {
			bad("integer out of range")
		}
		return int(n)
	}
	bad("not an integer")
	return 0
}

// toIntOrZero is int(v or 0).
func toIntOrZero(v any) int {
	if !truthy(v) {
		return 0
	}
	return toInt(v)
}

// toInt64 is int() for amounts in øre.
func toInt64(v any) int64 {
	if n, ok := v.(json.Number); ok {
		if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
			return i
		}
	}
	return int64(toInt(v))
}

// money is _money(): null stays missing, anything else must be a decimal number.
func money(v any) *model.Money {
	var text string
	switch x := v.(type) {
	case nil:
		return nil
	case json.Number:
		text = string(x)
	case string:
		text = x
	default:
		bad("not a number")
	}
	m, err := model.ParseMoney(text)
	if err != nil {
		bad("not a number")
	}
	return &m
}

// moneyOrZero is `_money(v) or Decimal(0)` for facts the model cannot leave empty.
func moneyOrZero(v any) model.Money {
	if m := money(v); m != nil {
		return *m
	}
	return model.Money{}
}

// requiredMoney is a price that must be there: a missing one is a format change, never 0 kr.
func requiredMoney(v any, what string) model.Money {
	m := money(v)
	if m == nil {
		bad("no %s", what)
	}
	return *m
}

// fromOre is Decimal(int(ore)) / 100.
func fromOre(v any) model.Money { return model.NewMoney(toInt64(v), -2) }

// rawStr is a value kept as it came: null stays null.
func rawStr(v any) *string {
	if v == nil {
		return nil
	}
	s := pyStr(v)
	return &s
}

// optStr is `v or None` for a text.
func optStr(v any) *string {
	if !truthy(v) {
		return nil
	}
	s := pyStr(v)
	return &s
}

// textOf is a string that must be a string when it is there ("" for nothing).
func textOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return pyStr(v)
}

func strPtr(s string) *string { return &s }

// sliceTo is a[:n] for a slice, with Python's meaning of a negative n.
func sliceTo[T any](a []T, n int) []T {
	if n < 0 {
		n = max(len(a)+n, 0)
	}
	if n > len(a) {
		n = len(a)
	}
	return a[:n]
}
