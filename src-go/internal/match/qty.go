package match

import (
	"math/big"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
)

// Qty is a count of packs. Python's int has no limit and the goldens use one: "99999999999999999999 x
// mælk" asks for 99999999999999999999 packs. An int64 would overflow, so a Qty wraps big.Int.
// A Qty is never changed after it is made. The zero value is 0.
type Qty struct{ v *big.Int }

// QtyOf makes a Qty from a small number.
func QtyOf(n int64) Qty { return Qty{big.NewInt(n)} }

// ParseQty reads digits, also Unicode ones ("٢" is 2), like Python's int(). ok is false
// when s is not all digits.
func ParseQty(s string) (Qty, bool) {
	if s == "" {
		return Qty{}, false
	}
	v, ok := new(big.Int).SetString(pystr.ASCIIDigits(s), 10)
	return Qty{v}, ok
}

func (q Qty) get() *big.Int {
	if q.v == nil {
		return new(big.Int)
	}
	return q.v
}

// Big returns a copy of the value.
func (q Qty) Big() *big.Int { return new(big.Int).Set(q.get()) }

// Cmp compares q and o: -1, 0 or 1.
func (q Qty) Cmp(o Qty) int { return q.get().Cmp(o.get()) }

// CmpInt compares q with a small number.
func (q Qty) CmpInt(n int64) int { return q.get().Cmp(big.NewInt(n)) }

// Sign is -1, 0 or 1.
func (q Qty) Sign() int { return q.get().Sign() }

// Add returns q + o.
func (q Qty) Add(o Qty) Qty { return Qty{new(big.Int).Add(q.get(), o.get())} }

// Sub returns q - o.
func (q Qty) Sub(o Qty) Qty { return Qty{new(big.Int).Sub(q.get(), o.get())} }

// Mul returns q * o.
func (q Qty) Mul(o Qty) Qty { return Qty{new(big.Int).Mul(q.get(), o.get())} }

// DivMod is Python's // and % for a positive o: q is rounded down, the rest is never negative.
func (q Qty) DivMod(o Qty) (Qty, Qty) {
	d, m := new(big.Int).DivMod(q.get(), o.get(), new(big.Int))
	return Qty{d}, Qty{m}
}

// Min is Python's min(q, o): q when they are equal.
func (q Qty) Min(o Qty) Qty {
	if o.Cmp(q) < 0 {
		return o
	}
	return q
}

// Money is the number as an exact amount, to multiply or divide kroner with.
func (q Qty) Money() model.Money { return model.MoneyFromBig(q.get()) }

// String is the number in digits.
func (q Qty) String() string { return q.get().String() }

// MarshalJSON writes a JSON number of any size.
func (q Qty) MarshalJSON() ([]byte, error) { return []byte(q.get().String()), nil }

// QtyFromBig wraps a whole number (a copy is not made: do not change it after).
func QtyFromBig(n *big.Int) Qty { return Qty{n} }
