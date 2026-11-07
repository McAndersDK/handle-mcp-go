// Package compare is compare_basket: price one shopping list in each store. A port of compare.py
// (ADR 0005).
//
// Simple and explainable. No store-specific code here. Words are matched in `match`, kroner are
// counted in `pricing`. This package picks the best product per item and compares the stores.
//   - Best word match first, then a comparable size, then the lowest cost for the amount.
//   - "2 x broccoli" with no size: the lowest cost for 2 packs wins, also 500 g against 1 stk.
//     Only for pieces and produce: "2 x mælk" still compares per litre.
//   - A product that matches only by its brand is never a guess: "Fior di Frutta" jam.
//   - Cold cuts and smoked ('Røget kyllingebryst') lose when the search also found it raw.
//   - On the same name score, a product in a matching store category wins ("Fisk > Laks").
//   - Then a product with a known size wins, then the lowest cost. Sold-out products never win.
//   - The caller picks from the candidates (package candidates): the suggestion is a guess by words.
//   - Unsure matches and missing items are marked, so a human can check them.
package compare

import (
	"fmt"
	"sort"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/candidates"
	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pricing"
)

// SearchLimit: cheap hits sit far down: tin 14 "hakkede tomater", chicken 30 "kyllingebryst".
const SearchLimit = 50

// BasketLine is one item from the list and the product picked for it.
type BasketLine struct {
	Item      string         `json:"item"` // the line from the shopping list
	Quantity  match.Qty      `json:"quantity"`
	Product   *model.Product `json:"product"`
	LineTotal model.Money    `json:"line_total"`
	Sure      bool           `json:"sure"`
	Note      *string        `json:"note"`
	Guess     bool           `json:"guess"` // the store's top hit with no word match: left out of the verdict
	// The suggested product and the next best, with flags: the caller picks (package candidates).
	Candidates model.List[candidates.Candidate] `json:"candidates"`
	Oeko       bool                             `json:"oeko"` // the chosen product is øko
}

// BasketQuote is one list priced in one store.
type BasketQuote struct {
	Store         string                  `json:"store"`
	Lines         model.List[BasketLine]  `json:"lines"`
	Missing       model.List[string]      `json:"missing"`
	ProductsTotal model.Money             `json:"products_total"`
	Fees          model.Dict[model.Money] `json:"fees"`
	Total         model.Money             `json:"total"`
	MinimumOrder  *model.Money            `json:"minimum_order"`
	MinimumMet    *bool                   `json:"minimum_met"`
	Notes         model.List[string]      `json:"notes"`
	DeliveryMode  *string                 `json:"delivery_mode"` // from the store's terms: "home", "pickup" or nil
	PickupStore   *string                 `json:"pickup_store"`
}

func (l BasketLine) asLine() candidates.Line {
	return candidates.Line{Product: l.Product, Quantity: l.Quantity, LineTotal: l.LineTotal, Note: l.Note, Sure: l.Sure}
}

// BestMatch is the best product for one list item, plus the candidates to pick from.
// The line's Product points into `products`.
func BestMatch(item string, products []model.Product, on *model.Date) BasketLine {
	line := best(item, products, on)
	_, words, _ := match.ParseItem(item)
	ask := match.Asked(item)
	type other struct {
		negScore float64
		total    model.Money
		line     BasketLine
	}
	var others []other
	for i := range products {
		x := &products[i]
		if !x.Available || (line.Product != nil && x.ID == line.Product.ID) {
			continue
		}
		alt := best(item, products[i:i+1], on)
		if alt.Product != nil && !alt.Guess {
			others = append(others, other{-match.Score(words, x, ask), alt.LineTotal, alt})
		}
	}
	sort.SliceStable(others, func(i, j int) bool { // key o[:2]
		a, b := others[i], others[j]
		if a.negScore != b.negScore {
			return a.negScore < b.negScore
		}
		return a.total.Cmp(b.total) < 0
	})
	alts := make([]candidates.Line, len(others))
	for i, o := range others {
		alts[i] = o.line.asLine()
	}
	line.Candidates = candidates.Candidates(item, line.asLine(), alts)
	line.Oeko = line.Product != nil && match.IsOeko(line.Product)
	return line
}

// or is Python's `x or fallback` on an optional string.
func or(s *string, fallback string) string {
	if s != nil && *s != "" {
		return *s
	}
	return fallback
}

// join is " ".join(filter(None, parts)).
func join(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " ")
}

// best: best word match first, then a comparable size, then the lowest cost for the amount.
// `on` is the delivery day: offers that end before it do not count.
func best(item string, products []model.Product, on *model.Date) BasketLine {
	quantity, words, size := match.ParseItem(item)
	ask := match.Asked(item)
	// The item is produce when the search found it fresh, by its full name: then canned or frozen
	// loses. Not "Spaghettisquash" for spaghetti.
	produce, raw := false, false
	for i := range products {
		x := &products[i]
		if x.Available && match.Score(words, x, nil) == 1 {
			produce = produce || match.IsFresh(x)
			// The search also found it raw ('Kyllingebrystfilet'): then cold cuts and smoked lose.
			raw = raw || !match.Prepared(words, x)
		}
	}
	var ranked []rank
	for i := range products {
		product := &products[i]
		nameScore := match.Score(words, product, ask)
		if !product.Available || nameScore < 0.5 {
			continue
		}
		nPacks, ok := pricing.Packs(size, quantity, product)
		if !ok {
			nPacks, ok = pricing.Pieces(size, quantity, product)
		}
		sizeRank := 1
		if ok || size == nil {
			sizeRank = 0
		}
		count := quantity
		if ok {
			count = nPacks
		}
		lineCost := pricing.Cost(product, count, on)
		// Canned or frozen produce loses before the category counts: a canned product can sit
		// in a category that fits ("... > Cherrytomater") while the fresh one does not.
		prep := 0
		if raw && match.Prepared(words, product) {
			prep = 1
		}
		fit := [4]int{match.ProcessedRank(words, product, produce), prep,
			match.CategoryRank(words, product), match.Mixed(words, product)}
		have := match.PackSize(product)
		amount := model.NewMoney(0, 0)
		if have != nil {
			amount = have.Amount.MulMoney(count.Money())
		}
		// '6 x bananer' or '2 x broccoli': N things. Compare what they cost, also a 500 g
		// broccoli against '1 stk'. Not per piece or kg. '2 x mælk' still compares per litre.
		byPiece := size == nil && quantity.CmpInt(1) > 0 && have != nil && (match.IsPiece(have.Base) || produce)
		r := rank{negScore: -nameScore, fit: fit, sizeRank: sizeRank, lineCost: lineCost,
			amount: amount, count: count, product: product}
		switch {
		case size != nil:
			r.single, r.price = true, lineCost
		case byPiece:
			r.price = lineCost
		default:
			u := pricing.PerUnit(product, quantity, count, on)
			r.kind, r.price = u.Kind, u.Value
		}
		ranked = append(ranked, r)
	}
	if len(ranked) == 0 {
		return guess(item, quantity, size, products, words, on)
	}
	// Same cost: more food wins, then fewer packs. Never the store's search order.
	top := &ranked[0]
	for i := 1; i < len(ranked); i++ {
		if ranked[i].less(top) {
			top = &ranked[i]
		}
	}
	product, count := top.product, top.count
	over := top.sizeRank == 0 && size != nil && pricing.TooMuch(size, quantity, count, product)
	isMixed := top.fit[3] == 1 // mixed() is always last in fit
	sure := top.negScore == -1.0 && top.sizeRank == 0 && !over && !isMixed
	var note string
	switch {
	case over:
		note = fmt.Sprintf("Anden størrelse: %s x %s.", count, or(product.Size, product.Name))
	case top.sizeRank == 1:
		note = fmt.Sprintf("Størrelsen kan ikke sammenlignes: %s.", or(product.Size, product.Name))
	case isMixed:
		note = fmt.Sprintf("Blandingsvare: %s. Tjek varen.", product.Name)
	case !sure:
		note = "Usikkert match. Tjek varen."
	case size != nil && count.Cmp(quantity) != 0:
		note = fmt.Sprintf("%s x %s for at nå mængden.", count, or(product.Size, "pakke"))
	}
	have := match.PackSize(product)
	if size == nil && quantity.CmpInt(1) > 0 && have != nil && match.IsPiece(have.Base) {
		total := have.Amount.MulMoney(count.Money())
		if total.Cmp(quantity.Money()) > 0 {
			note = join(note, fmt.Sprintf("%s x %s = %s stk.", count, or(product.Size, "pakke"), total.FormatG()))
		}
	} else if size == nil {
		what := "pr. kg, l eller stk"
		if quantity.CmpInt(1) > 0 && produce {
			what = quantity.String() + " pakker"
		}
		note = join(note, fmt.Sprintf("Ingen mængde på listen: billigst %s. Skriv en mængde, fx '1 kg'.", what))
	}
	if hint := pricing.MultiHint(product, count, on); hint != nil {
		note = join(note, *hint)
	} else {
		note = join(note)
	}
	if until := product.OfferUntil; on != nil && until != nil && on.Compare(*until) > 0 &&
		product.BeforePrice != nil && !product.BeforePrice.IsZero() {
		_, m, d := until.YMD()
		note = join(note, fmt.Sprintf("Tilbuddet slutter %02d/%02d, før levering.", d, int(m)))
	}
	return BasketLine{Item: item, Quantity: count, Product: product, LineTotal: top.lineCost, Sure: sure, Note: &note}
}
