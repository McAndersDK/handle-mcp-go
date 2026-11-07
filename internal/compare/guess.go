package compare

import (
	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pricing"
)

// guess: no name matches, e.g. "Penne" for "pasta". Never sure and left out of the verdict.
// With a size: only products of that size kind, cheapest for the amount ("Tortelloni 250 g"
// is not a guess for "pasta 500 g" when "Fettuccine 500 g" is cheaper). Without: the store's
// top hit. Never a flavour-only hit ("Grøn te m. mandarin" for "mandariner").
func guess(item string, quantity match.Qty, size *match.Size, products []model.Product, words []string, on *model.Date) BasketLine {
	notFound := func() BasketLine {
		note := "Ikke fundet"
		return BasketLine{Item: item, Quantity: quantity, LineTotal: model.NewMoney(0, 0), Note: &note}
	}
	var ok []*model.Product
	for i := range products {
		x := &products[i]
		if x.Available && !match.FlavourOnly(words, x) && !match.BrandOnly(words, x) {
			ok = append(ok, x)
		}
	}
	var top *model.Product
	count := quantity
	if size != nil {
		var topCost model.Money
		for _, x := range ok {
			n, found := pricing.Packs(size, quantity, x)
			if !found || n.Sign() == 0 {
				continue
			}
			if c := pricing.Cost(x, n, on); top == nil || c.Cmp(topCost) < 0 {
				top, count, topCost = x, n, c
			}
		}
		if top == nil {
			return notFound()
		}
	} else if len(ok) > 0 {
		top = ok[0]
	} else {
		return notFound()
	}
	note := "Butikkens bedste søgeresultat. Navnet matcher ikke. Tjek varen."
	return BasketLine{Item: item, Quantity: count, Product: top, LineTotal: pricing.Cost(top, count, on),
		Note: &note, Guess: true}
}
