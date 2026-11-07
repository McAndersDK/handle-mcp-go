package model

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// The arithmetic the matcher and the pricing need. None of it rounds, except Round.

// Sub returns m - o, exact.
func (m Money) Sub(o Money) Money { return Money{d: m.d.Sub(o.d)} }

// MulMoney returns m * o, exact.
func (m Money) MulMoney(o Money) Money { return Money{d: m.d.Mul(o.d)} }

// IsZero tells whether m is 0 (Python: a falsy Decimal).
func (m Money) IsZero() bool { return m.d.IsZero() }

// MoneyFromBig makes a Money from a whole number of any size.
func MoneyFromBig(n *big.Int) Money { return Money{d: decimal.NewFromBigInt(n, 0)} }

// Ceil is math.ceil on a Decimal: exact, never through a float.
func (m Money) Ceil() *big.Int { return m.d.Ceil().BigInt() }

// Round rounds half up (away from zero) to the given number of decimals. For tests of
// unit-level numbers; answers are rounded by String.
func (m Money) Round(places int32) Money { return Money{d: m.d.Round(places)} }

// FormatG is Python's format(Decimal, "g") for a number without an exponent: the digits as
// they are, trailing zeros kept ("6", "2.50", "1500.0").
func (m Money) FormatG() string {
	e := m.d.Exponent()
	if e > 0 {
		return pyString(m.d)
	}
	return m.d.StringFixed(-e)
}

// Compare is -1, 0 or 1. Dates compare by calendar day.
func (d Date) Compare(o Date) int {
	switch {
	case d.y != o.y:
		return cmpInt(d.y, o.y)
	case d.m != o.m:
		return cmpInt(d.m, o.m)
	}
	return cmpInt(d.d, o.d)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Equal is Python's == on two Products: every field, and Decimals by value ("1.0" == "1.00").
func (p Product) Equal(o Product) bool {
	return p.Store == o.Store && p.ID == o.ID && p.Name == o.Name && p.Price.Cmp(o.Price) == 0 &&
		samePtr(p.Brand, o.Brand) && samePtr(p.Size, o.Size) && sameMoney(p.UnitPrice, o.UnitPrice) &&
		samePtr(p.Unit, o.Unit) && samePtr(p.EAN, o.EAN) && p.OnOffer == o.OnOffer &&
		samePtr(p.OfferText, o.OfferText) && sameMoney(p.BeforePrice, o.BeforePrice) &&
		sameDate(p.OfferUntil, o.OfferUntil) && p.Available == o.Available &&
		samePtr(p.Category, o.Category) && samePtr(p.Campaign, o.Campaign) &&
		samePtr(p.MaxPerOrder, o.MaxPerOrder)
}

func samePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameMoney(a, b *Money) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(*b) == 0
}

func sameDate(a, b *Date) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Compare(*b) == 0
}
