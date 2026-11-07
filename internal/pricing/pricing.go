// Package pricing says what an item costs in one store: packs for the amount, offers and the
// delivery day. A port of pricing.py (ADR 0005).
//
//   - A size like "1,5 kg" is the amount wanted: a 500 g pack is bought 3 times. Never less than
//     wanted (990 g is 1 kg, 900 g is not); up to 15 % more still counts as the same size.
//   - "6 x bananer" with no size means 6 bananas: a 4-pack is bought twice.
//   - An offer is gone after its last day, so prices are for the delivery day.
//   - A multi-buy offer ("3 for 48 kr") counts for the packs we need, also in the price per kg.
//     Packs over the offer's limit ("max 1 stk.") cost the normal price.
//   - No size in the item: price per kg, l or piece, so packs of different sizes compare.
//
// `on` is the delivery day; nil means no day is known, so no offer has ended.
package pricing

import (
	"fmt"
	"math"
	"math/big"

	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

var (
	// sizeTolerance: up to 15 % MORE than wanted still counts as the same size.
	sizeTolerance = model.NewMoney(15, -2)
	// shortTolerance: never less than wanted: 990 g is 1 kg, 900 g is not.
	shortTolerance = model.NewMoney(2, -2)
	one            = model.NewMoney(1, 0)
)

// ended tells whether the offer is over on the delivery day: `on and until and on > until`.
func ended(p *model.Product, on *model.Date) bool {
	return on != nil && p.OfferUntil != nil && on.Compare(*p.OfferUntil) > 0
}

// truthy is Python's truth of a Decimal: not 0 (and not None).
func truthy(m *model.Money) bool { return m != nil && !m.IsZero() }

// price is the price on the delivery day: the offer is gone after its last day.
func price(p *model.Product, on *model.Date) model.Money {
	if ended(p, on) && truthy(p.BeforePrice) {
		return *p.BeforePrice
	}
	return p.Price
}

// offer is a multi-buy: `Size` packs cost `Total`.
type offer struct {
	size  match.Qty
	total model.Money
}

// multi is a multi-buy offer like '4 for 50 kr' that is still on: (4, 50). It is gone after
// its last day even when the store sends no before-price.
func multi(p *model.Product, on *model.Date) *offer {
	size, total, ok := findMulti(str(p.OfferText))
	if !ok || ended(p, on) {
		return nil
	}
	n, ok1 := match.ParseQty(size)
	t, err := model.ParseMoney(replaceComma(pystr.ASCIIDigits(total)))
	if !ok1 || err != nil {
		return nil
	}
	if n.CmpInt(1) > 0 && t.Cmp(price(p, on).MulMoney(n.Money())) < 0 {
		return &offer{n, t}
	}
	return nil
}

// Cost is the price for `count` packs, with a multi-buy offer like '4 for 50 kr' when it is still on.
// Packs over the offer's limit ('max 1 stk.', e.g. a free gift) cost the normal price.
func Cost(p *model.Product, count match.Qty, on *model.Date) model.Money {
	pr := price(p, on)
	offered := count
	if p.MaxPerOrder != nil && *p.MaxPerOrder != 0 { // the limit caps multi-buys too
		offered = count.Min(match.QtyOf(int64(*p.MaxPerOrder)))
	}
	normal := pr
	if truthy(p.BeforePrice) {
		normal = *p.BeforePrice
	}
	var cost model.Money
	if o := multi(p, on); o != nil {
		whole, rest := offered.DivMod(o.size)
		cost = o.total.MulMoney(whole.Money()).Add(pr.MulMoney(rest.Money()))
	} else {
		cost = pr.MulMoney(offered.Money())
	}
	return cost.Add(normal.MulMoney(count.Sub(offered).Money()))
}

// MultiHint says it when the multi-buy is close: 2 packs at 22 kr, but '3 for 48 kr' is 4 kr more.
// We never buy more than needed on our own: the human decides.
func MultiHint(p *model.Product, count match.Qty, on *model.Date) *string {
	o := multi(p, on)
	if o == nil {
		return nil
	}
	if _, rest := count.DivMod(o.size); rest.Sign() == 0 {
		return nil
	}
	full := ceilDiv(count, o.size).Mul(o.size)
	extra := Cost(p, full, on).Sub(Cost(p, count, on))
	if extra.Cmp(price(p, on)) >= 0 {
		return nil
	}
	s := fmt.Sprintf("Tilbud %s: %s mere for %s kr.", str(p.OfferText), full.Sub(count), extra)
	return &s
}

// ceilDiv is math.ceil(count / size). Python divides two ints with a float, so this does too.
func ceilDiv(count, size match.Qty) match.Qty {
	f, _ := new(big.Rat).SetFrac(count.Big(), size.Big()).Float64()
	c := math.Ceil(f)
	if math.IsInf(c, 0) { // Python raises OverflowError here; the exact answer is better than a crash
		q, r := count.DivMod(size)
		if r.Sign() != 0 {
			q = q.Add(match.QtyOf(1))
		}
		return q
	}
	n, _ := new(big.Float).SetFloat64(c).Int(nil)
	return match.QtyFromBig(n)
}

// Kind of a per-unit price: 0 and 1 compare apart, 2 is a price per pack.
type UnitPrice struct {
	Kind  int
	Value model.Money
}

// PerUnit: for an item with no size: compare price per kg, l or piece, not per pack.
// '4 x pærer' counts pieces, so pieces come first. 'æbler' is a bag, so weight comes first.
// The pack price is what `count` packs cost each, so '3 for 48 kr' counts for 3 packs.
func PerUnit(p *model.Product, quantity, count match.Qty, on *model.Date) UnitPrice {
	have := match.PackSize(p)
	total := Cost(p, count, on)
	each, err := total.Div(count.Money())
	if err != nil { // count is never 0; the total is the safe answer if it ever is
		each = total
	}
	if have == nil || have.Amount.IsZero() {
		return UnitPrice{2, each}
	}
	byPiece := match.IsPiece(have.Base)
	kind := 0
	if quantity.CmpInt(1) > 0 {
		if !byPiece {
			kind = 1
		}
	} else if byPiece {
		kind = 1
	}
	v, err := each.Div(have.Amount)
	if err != nil {
		v = each
	}
	return UnitPrice{kind, v}
}

// Packs: how many packs give the wanted amount. '1,5 kg' with 500 g packs → 3.
// ok is false when the sizes cannot be compared (no size, or kg against stk).
func Packs(wanted *match.Size, quantity match.Qty, p *model.Product) (match.Qty, bool) {
	have := match.PackSize(p)
	if wanted == nil || have == nil || have.Amount.IsZero() {
		return match.Qty{}, false
	}
	// '8 stk' toilet paper takes rolls. '8 ruller' never takes '42 stk' wet wipes.
	unit := have.Base
	if have.Base == "rl" && wanted.Base == "stk" {
		unit = "stk"
	}
	if wanted.Base != unit {
		return match.Qty{}, false
	}
	need := wanted.Amount.MulMoney(quantity.Money()).MulMoney(one.Sub(shortTolerance))
	q, err := need.Div(have.Amount)
	if err != nil {
		return match.Qty{}, false
	}
	n := match.QtyFromBig(q.Ceil())
	if n.CmpInt(1) < 0 {
		n = match.QtyOf(1)
	}
	return n, true
}

// Pieces: '6 x bananer' with no size means 6 bananas: a 4-pack is bought twice, not 6 times.
func Pieces(wanted *match.Size, quantity match.Qty, p *model.Product) (match.Qty, bool) {
	have := match.PackSize(p)
	if wanted != nil || quantity.CmpInt(2) < 0 || have == nil || !match.IsPiece(have.Base) ||
		have.Amount.Cmp(one) <= 0 {
		return match.Qty{}, false
	}
	q, err := quantity.Money().Div(have.Amount)
	if err != nil {
		return match.Qty{}, false
	}
	return match.QtyFromBig(q.Ceil()), true
}

// TooMuch is true when the packs give over 15 % more than wanted: 500 g for '400 g'.
func TooMuch(wanted *match.Size, quantity, count match.Qty, p *model.Product) bool {
	have := match.PackSize(p)
	if have == nil {
		return false
	}
	limit := wanted.Amount.MulMoney(quantity.Money()).MulMoney(one.Add(sizeTolerance))
	return have.Amount.MulMoney(count.Money()).Cmp(limit) > 0
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func replaceComma(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c == ',' {
			b[i] = '.'
		}
	}
	return string(b)
}
