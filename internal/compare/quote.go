package compare

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // offers end on Danish days, also on a machine with no zone database

	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// copenhagen is the zone offers end in. The embedded zone data makes the load safe.
var copenhagen = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Copenhagen"); err == nil {
		return loc
	}
	return time.FixedZone("CET", 3600)
}()

// Search is search_products for one store: the store's own order, or the cheapest of SearchLimit
// hits first by `price` or `unit_price` (no unit price: last). Stores rank by relevance, so the
// cheapest chicken was hit 30 of 32 for "kyllingebryst" (nemlig, 2026-11-03).
func Search(ctx context.Context, s store.Store, text string, limit int, sortBy string) ([]model.Product, error) {
	if sortBy == "relevance" {
		return s.Search(ctx, text, limit)
	}
	if sortBy != "price" && sortBy != "unit_price" {
		return nil, store.Errorf("Ukendt sortering: %s.", sortBy)
	}
	hits, err := s.Search(ctx, text, max(limit, SearchLimit))
	if err != nil {
		return nil, err
	}
	key := func(p *model.Product) (missing bool, v model.Money) {
		if sortBy == "price" {
			return false, p.Price
		}
		if p.UnitPrice == nil {
			return true, model.Money{}
		}
		return false, *p.UnitPrice // an unit price of 0 is 0 either way
	}
	sort.SliceStable(hits, func(i, j int) bool {
		mi, vi := key(&hits[i])
		mj, vj := key(&hits[j])
		if mi != mj {
			return !mi
		}
		return vi.Cmp(vj) < 0
	})
	return hits[:min(max(limit, 0), len(hits))], nil
}

// Quote searches each item in one store (one call at a time) and adds fees and minimum order.
func Quote(ctx context.Context, s store.Store, items []string) (BasketQuote, error) {
	// Read-only; e.g. a fee that depends on the slot. Prices still count without it.
	var note *string
	var on *model.Date
	info, err := s.DeliveryInfo(ctx)
	switch {
	case err == nil:
		note, on = info.Note, day(info)
	case isSoft(err):
	default:
		return BasketQuote{}, err
	}
	lines := make([]BasketLine, 0, len(items))
	for _, item := range items {
		products, err := s.Search(ctx, match.Query(item), SearchLimit)
		if err != nil {
			return BasketQuote{}, err
		}
		lines = append(lines, BestMatch(item, products, on))
	}
	terms, err := s.Terms(ctx)
	if err != nil {
		return BasketQuote{}, err
	}
	var found []BasketLine
	var missing []string
	productsTotal := model.NewMoney(0, 0)
	for _, ln := range lines {
		if ln.Product == nil {
			missing = append(missing, ln.Item)
			continue
		}
		found = append(found, ln)
		productsTotal = productsTotal.Add(ln.LineTotal)
	}
	allFees := map[string]model.Money{}
	if terms.DeliveryFee != nil {
		allFees["levering"] = *terms.DeliveryFee
	}
	for k, v := range terms.OtherFees {
		allFees[k] = v
	}
	fees := model.Dict[model.Money]{}
	total := productsTotal
	for k, v := range allFees {
		if !v.IsZero() { // a fee of 0 is left out
			fees[k] = v
			total = total.Add(v)
		}
	}
	var notes []string
	hasNote := note != nil && *note != ""
	if hasNote {
		notes = append(notes, *note)
	}
	switch {
	case terms.DeliveryMode != nil && *terms.DeliveryMode == model.DeliveryPickup:
		notes = append(notes, pickupNote(terms.PickupStore))
	case terms.DeliveryMode == nil && !terms.HomeDelivery && !hasNote: // the old note, only when the mode is unknown
		notes = append(notes, "Kurven er ikke sat til hjemlevering.")
	}
	minimum := terms.MinimumOrder
	if minimum != nil && productsTotal.Sign() > 0 && productsTotal.Cmp(*minimum) < 0 { // in words, for the human
		notes = append(notes, fmt.Sprintf("Under minimumskøb: %s kr. mangler til %s kr.", minimum.Sub(productsTotal), minimum))
	}
	q := BasketQuote{
		Store: terms.Store, Lines: found, Missing: missing, ProductsTotal: productsTotal,
		Fees: fees, Total: total, MinimumOrder: minimum, Notes: notes,
		DeliveryMode: terms.DeliveryMode, PickupStore: terms.PickupStore,
	}
	if minimum != nil {
		met := productsTotal.Cmp(*minimum) >= 0
		q.MinimumMet = &met
	}
	return q, nil
}

// isSoft is the error of a delivery_info call that does not stop the quote: the store cannot
// do it (NotImplementedError) or says no (StoreError).
func isSoft(err error) bool {
	var se *store.StoreError
	return errors.Is(err, store.ErrNotImplemented) || errors.As(err, &se)
}

// day is the delivery day: the chosen slot, else the next free one.
func day(info model.DeliveryInfo) *model.Date {
	if start := info.ChosenStart; start != nil {
		d := start.Date() // a time with no zone keeps its own day
		if start.Aware() {
			y, m, dd := start.Time().In(copenhagen).Date()
			if date, err := model.NewDate(y, m, dd); err == nil {
				d = date
			}
		}
		return &d
	}
	return info.NextAvailable
}

// VerdictAnswer is the comparison of the stores on the items every store has, plus each store's fees.
// "stores" is added by the caller (the tool), next to these fields.
type VerdictAnswer struct {
	Cheapest         *string                `json:"cheapest"`
	ComparableTotals map[string]model.Money `json:"comparable_totals,omitempty"`
	Summary          string                 `json:"summary"`
}

// Verdict compares the quotes. `results` are in the requested store order; a store with an error did not
// price the list.
func Verdict(results []Result[BasketQuote]) VerdictAnswer {
	var ok []Result[BasketQuote]
	var failedNames, okNames []string
	for _, r := range results {
		if r.Err != nil {
			failedNames = append(failedNames, r.Name)
		} else {
			ok = append(ok, r)
			okNames = append(okNames, r.Name)
		}
	}
	failed := strings.Join(failedNames, ", ")
	if len(ok) < 2 {
		priced := "Ingen butik kunne prise listen."
		if len(ok) > 0 {
			priced = fmt.Sprintf("Kun %s kunne prise listen.", strings.Join(okNames, ", "))
		}
		if failed != "" {
			priced += fmt.Sprintf(" Fejl: %s.", failed)
		}
		return VerdictAnswer{Summary: priced}
	}
	var common map[string]bool
	for _, r := range ok {
		own := map[string]bool{}
		for _, ln := range r.Value.Lines {
			if !ln.Guess {
				own[ln.Item] = true
			}
		}
		if common == nil {
			common = own
			continue
		}
		for item := range common {
			if !own[item] {
				delete(common, item)
			}
		}
	}
	if len(common) == 0 {
		return VerdictAnswer{Summary: "Ingen vare blev fundet i alle butikker. Tjek listen."}
	}
	totals := map[string]model.Money{}
	cheapest := ""
	for i, r := range ok {
		sum := model.NewMoney(0, 0)
		for _, ln := range r.Value.Lines {
			if common[ln.Item] {
				sum = sum.Add(ln.LineTotal)
			}
		}
		for _, fee := range r.Value.Fees {
			sum = sum.Add(fee)
		}
		totals[r.Name] = sum
		if i == 0 || sum.Cmp(totals[cheapest]) < 0 { // the first of equals wins
			cheapest = r.Name
		}
	}
	summary := fmt.Sprintf("Billigst: %s (%s kr. inkl. gebyrer", cheapest, totals[cheapest])
	summary += fmt.Sprintf(" for de %d varer, alle butikker har).", len(common))
	unsure := false
	for _, r := range ok {
		unsure = unsure || len(r.Value.Missing) > 0
		for _, ln := range r.Value.Lines {
			unsure = unsure || !ln.Sure
		}
	}
	if unsure {
		summary += " Nogle varer mangler eller er usikre. Tjek dem."
	}
	if failed != "" {
		summary += fmt.Sprintf(" Fejl: %s.", failed)
	}
	summary += modeSentence(ok, totals)
	return VerdictAnswer{Cheapest: &cheapest, ComparableTotals: totals, Summary: summary}
}
