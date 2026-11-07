package compare

import (
	"fmt"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// pickupNote tells the human that a pickup quote is not a delivery quote (docs/adr/0006).
func pickupNote(pickupStore *string) string {
	if pickupStore == nil || *pickupStore == "" {
		return "Afhentning: butikken er ikke oplyst."
	}
	return fmt.Sprintf("Afhentning i %s: du henter selv varerne.", *pickupStore)
}

// modeSentence is added to the verdict when the stores that priced the list do not share one way
// of getting the groceries: the cheapest with delivery and the cheapest with pickup, side by side.
// It says nothing when all stores have one mode, or when any store's mode is unknown (nil): a
// guess about a mode must not read as a fact. `cheapest` is not changed by it; it stays money only.
func modeSentence(ok []Result[BasketQuote], totals map[string]model.Money) string {
	var home, pickup string // the cheapest store of each mode; the first of equals wins
	for _, r := range ok {
		mode := r.Value.DeliveryMode
		if mode == nil {
			return ""
		}
		target := &home
		switch *mode {
		case model.DeliveryHome:
		case model.DeliveryPickup:
			target = &pickup
		default:
			return ""
		}
		if *target == "" || totals[r.Name].Cmp(totals[*target]) < 0 {
			*target = r.Name
		}
	}
	if home == "" || pickup == "" {
		return ""
	}
	return fmt.Sprintf(" Billigst med levering: %s (%s kr.). Billigst med afhentning: %s (%s kr.). Turen til butikken er ikke regnet med.",
		home, totals[home], pickup, totals[pickup])
}
