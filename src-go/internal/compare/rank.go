package compare

import (
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
)

// rank is one row of `ranked` in _best: the first seven fields are the sort key.
type rank struct {
	negScore float64
	fit      [4]int
	sizeRank int
	single   bool // value is (line_cost,), else (kind, price)
	kind     int
	price    model.Money
	lineCost model.Money
	amount   model.Money // the key is -amount
	count    match.Qty
	product  *model.Product
}

// less is Python's tuple comparison of the first seven fields.
func (a *rank) less(b *rank) bool {
	if a.negScore != b.negScore {
		return a.negScore < b.negScore
	}
	for i := range a.fit {
		if a.fit[i] != b.fit[i] {
			return a.fit[i] < b.fit[i]
		}
	}
	if a.sizeRank != b.sizeRank {
		return a.sizeRank < b.sizeRank
	}
	if !a.single && a.kind != b.kind {
		return a.kind < b.kind
	}
	if c := a.price.Cmp(b.price); c != 0 {
		return c < 0
	}
	if c := a.lineCost.Cmp(b.lineCost); c != 0 {
		return c < 0
	}
	if c := b.amount.Cmp(a.amount); c != 0 { // -amount: more food first
		return c < 0
	}
	return a.count.Cmp(b.count) < 0
}
