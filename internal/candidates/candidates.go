// Package candidates lists the products for one list item that the caller (an AI model or a
// human) picks from. A port of candidates.py (ADR 0005).
//
// The matcher only suggests. Each candidate carries plain facts, so the caller can decide with
// the user's own taste instead of hidden word rules: is 'Babymajs' fine, is øko worth it?
//   - The suggested product comes first, then the next best, at most Limit in all.
//   - An øko product is always among them when the search found one.
//   - Flags say what the matcher saw: name match, size fits, prepared, processed, blend, øko.
//     An unknown fact or a flag that is false is left out, to keep the answer small.
//     `size_fits` is shown when false: the size does not fit.
package candidates

import (
	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pricing"
)

// Limit is how many candidates an item gets, at most.
const Limit = 5

// Line is what the candidates need to know about a basket line (compare.BasketLine).
type Line struct {
	Product   *model.Product
	Quantity  match.Qty
	LineTotal model.Money
	Note      *string
	Sure      bool
}

// Equal is Python's == on two basket lines made for the same item: the fields that can differ.
func (l Line) Equal(o Line) bool {
	if (l.Product == nil) != (o.Product == nil) || (l.Product != nil && !l.Product.Equal(*o.Product)) {
		return false
	}
	sameNote := (l.Note == nil && o.Note == nil) || (l.Note != nil && o.Note != nil && *l.Note == *o.Note)
	return l.Quantity.Cmp(o.Quantity) == 0 && l.LineTotal.Cmp(o.LineTotal) == 0 &&
		l.Sure == o.Sure && sameNote
}

// Candidate is one product for the caller. A nil pointer or a false flag is left out of the
// answer; an empty string is kept (it is a fact, not a missing one).
type Candidate struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Brand     *string      `json:"brand,omitempty"`
	Size      *string      `json:"size,omitempty"`
	Price     model.Money  `json:"price"`
	Quantity  match.Qty    `json:"quantity"`
	LineTotal model.Money  `json:"line_total"`
	UnitPrice *model.Money `json:"unit_price,omitempty"` // per kg, l or stk, from the store
	Unit      *string      `json:"unit,omitempty"`
	OfferText *string      `json:"offer_text,omitempty"`
	Category  *string      `json:"category,omitempty"` // 'Konserves': a human (and the live list) sees canned
	Oeko      bool         `json:"oeko,omitempty"`
	Note      *string      `json:"note,omitempty"`
	Suggested bool         `json:"suggested,omitempty"`
	NameMatch string       `json:"name_match"`
	SizeFits  *bool        `json:"size_fits,omitempty"` // nil: the list gives no size
	Prepared  bool         `json:"prepared,omitempty"`  // cold cuts, smoked, breaded
	Processed *string      `json:"processed,omitempty"` // "frost", "konserves" or nil
	Blend     bool         `json:"blend,omitempty"`     // 'Hakket oksekød m. 50% grønt'
}

// Candidates: `line` is the suggested BasketLine, `others` the next best lines, best first.
func Candidates(item string, line Line, others []Line) model.List[Candidate] {
	if line.Product == nil {
		return nil
	}
	picked := others[:min(len(others), Limit-1)]
	if !match.IsOeko(line.Product) {
		for _, a := range others {
			if !match.IsOeko(a.Product) {
				continue
			}
			found := false // Python: `oeko[0] not in picked`, with == on the lines
			for _, b := range picked {
				found = found || a.Equal(b)
			}
			if !found {
				picked = append(append([]Line(nil), picked[:min(len(picked), Limit-2)]...), a)
			}
			break // only the first øko line counts
		}
	}
	out := model.List[Candidate]{Brief(item, line, true)}
	for _, a := range picked {
		out = append(out, Brief(item, a, false))
	}
	return out
}

// Brief is one product for the caller: what it costs for the amount, and the flags.
func Brief(item string, line Line, suggested bool) Candidate {
	p := line.Product
	quantity, words, size := match.ParseItem(item)
	nameScore := match.Score(words, p, match.Asked(item))
	var fits *bool
	if size != nil {
		if _, ok := pricing.Packs(size, quantity, p); ok { // not ok: no size, or kg against stk. Unknown, not false.
			f := !pricing.TooMuch(size, quantity, line.Quantity, p)
			fits = &f
		}
	} else if have := match.PackSize(p); quantity.CmpInt(1) > 0 && have != nil && match.IsPiece(have.Base) {
		t := true // '6 x bananer': pieces fit, a 1 kg bag cannot be compared
		fits = &t
	}
	nameMatch := "none"
	switch {
	case nameScore == 1:
		nameMatch = "full"
	case nameScore >= 0.5:
		nameMatch = "partial"
	}
	return Candidate{
		ID: p.ID, Name: p.Name, Brand: p.Brand, Size: p.Size, Price: p.Price,
		Quantity: line.Quantity, LineTotal: line.LineTotal,
		UnitPrice: p.UnitPrice, Unit: p.Unit, OfferText: p.OfferText, Category: p.Category,
		Oeko: match.IsOeko(p), Note: line.Note, Suggested: suggested,
		NameMatch: nameMatch, SizeFits: fits,
		Prepared: match.Prepared(words, p), Processed: match.Processed(p),
		Blend: match.Mixed(words, p) != 0,
	}
}
