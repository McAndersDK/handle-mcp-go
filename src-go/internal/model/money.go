// Package model holds the few shapes every store returns. Money is exact kroner, never float.
package model

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// maxExp stops a store from sending 1e999999999 and making us build a huge number.
const maxExp = 1000

// maxDigits is Python's decimal context precision. A rounded value with more digits
// is shown as the store wrote it (ADR 0002), never dropped.
const maxDigits = 28

// ErrDivZero is returned by Money.Div.
var ErrDivZero = errors.New("division by zero")

// Money is a decimal amount in DKK. It never rounds internally: rounding to two
// decimals (half up, away from zero) happens only when it is marshalled, as the
// string "39.00" (ADR 0002). The zero value is 0.
type Money struct {
	d decimal.Decimal
	// negZero keeps the sign of an input like "-0", which Python prints as "-0.00".
	negZero bool
}

// ParseMoney reads a decimal text such as "12.95", "1E+2" or "0.0". NaN and Infinity
// are errors: a decimal cannot hold them (ADR 0005). The caller turns that into a StoreError.
func ParseMoney(s string) (Money, error) {
	s = strings.TrimSpace(s)
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Money{}, fmt.Errorf("not a decimal number: %q", s)
	}
	if e := d.Exponent(); e > maxExp || e < -maxExp {
		return Money{}, fmt.Errorf("decimal number out of range: %q", s)
	}
	return Money{d: d, negZero: d.IsZero() && strings.HasPrefix(s, "-")}, nil
}

// NewMoney makes a Money from whole kroner and øre-less cents, e.g. NewMoney(1295, -2) is 12.95.
func NewMoney(value int64, exp int32) Money { return Money{d: decimal.New(value, exp)} }

// Add returns m + o, exact.
func (m Money) Add(o Money) Money { return Money{d: m.d.Add(o.d)} }

// Mul returns m * n, exact.
func (m Money) Mul(n int64) Money { return Money{d: m.d.Mul(decimal.NewFromInt(n))} }

// Div returns m / o with 28 decimals, the same precision Python's Decimal division has
// for kroner amounts.
func (m Money) Div(o Money) (Money, error) {
	if o.d.IsZero() {
		return Money{}, ErrDivZero
	}
	return Money{d: m.d.DivRound(o.d, maxDigits)}, nil
}

// Cmp compares m and o: -1, 0 or 1.
func (m Money) Cmp(o Money) int { return m.d.Cmp(o.d) }

// Sign is -1, 0 or 1.
func (m Money) Sign() int { return m.d.Sign() }

// String is the answer form: two decimals, e.g. "39.00".
func (m Money) String() string {
	q := m.d.Round(2) // half away from zero, i.e. half up for kroner
	digits := len(q.Shift(2).Abs().BigInt().String())
	if digits > maxDigits { // Python cannot quantize this: show it as it is
		return pyString(m.d)
	}
	s := q.StringFixed(2)
	if (m.negZero || m.d.Sign() < 0) && q.IsZero() {
		s = "-" + s
	}
	return s
}

// MarshalJSON writes the two-decimal string.
func (m Money) MarshalJSON() ([]byte, error) { return []byte(`"` + m.String() + `"`), nil }

// UnmarshalJSON accepts a JSON number or string. The text goes straight to the
// decimal parser, never through float64.
func (m *Money) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	b = bytes.Trim(b, `"`)
	v, err := ParseMoney(string(b))
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// pyString writes d like Python's str(Decimal): plain digits, or 1E+40 for a positive exponent.
func pyString(d decimal.Decimal) string {
	coef, exp := d.Coefficient().String(), int(d.Exponent())
	sign := ""
	if strings.HasPrefix(coef, "-") {
		sign, coef = "-", coef[1:]
	}
	adjusted := exp + len(coef) - 1
	if exp <= 0 && adjusted >= -6 {
		if exp == 0 {
			return sign + coef
		}
		if len(coef) <= -exp {
			coef = strings.Repeat("0", -exp-len(coef)+1) + coef
		}
		return sign + coef[:len(coef)+exp] + "." + coef[len(coef)+exp:]
	}
	mant := coef[:1]
	if len(coef) > 1 {
		mant += "." + coef[1:]
	}
	return fmt.Sprintf("%s%sE%+d", sign, mant, adjusted)
}
