// Package recipecart is add_recipe_to_cart: which products a recipe adds to the cart, and the new
// absolute counts. A port of recipe_cart.py (ADR 0005).
//
// Pure functions: the tool fetches the recipe and the cart, and writes with the store's
// SetQuantities (absolute counts). The store's own "add recipe" call is never used.
package recipecart

import (
	"fmt"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

// PlanError is a skip/swap/include the recipe does not have. The text is for the human.
type PlanError struct{ Msg string }

func (e *PlanError) Error() string { return e.Msg }

// Swap replaces the product of one ingredient (Key) by one of its alternatives (Value, a product id).
// A slice, not a map: Python's dict keeps the order, and the first key that fits an ingredient wins.
type Swap struct{ Key, Value string }

// Line is one product to add: the ingredients it covers and the packs in all.
type Line struct {
	ProductID   string   `json:"product_id"`
	Ingredients []string `json:"ingredients"`
	Name        string   `json:"name"`
	Quantity    int      `json:"quantity"`
}

// Skipped is an ingredient that is left out, and why.
type Skipped struct {
	Ingredient string `json:"ingredient"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
}

func matches(key string, item model.RecipeItem) bool {
	key = pystr.Lower(pystr.Strip(key))
	return key == pystr.Lower(pystr.Strip(item.Ingredient)) || key == item.Product.ID
}

func anyMatch(keys []string, item model.RecipeItem) bool {
	for _, k := range keys {
		if matches(k, item) {
			return true
		}
	}
	return false
}

// Plan returns the products to add (in the order of the recipe, one per product id) and the
// skipped items with a reason.
//
// have_at_home items (salt, oil) are skipped unless in `include`. `skip` and the keys of
// `swap`/`include` name an ingredient ("Hakket grise- og kalvekød") or a product id; a swap
// value must be one of that ingredient's alternatives. Unknown names are a *PlanError.
func Plan(recipe model.RecipeDetails, skip []string, swap []Swap, include []string) ([]Line, []Skipped, error) {
	items := recipe.Items
	var keys []string
	keys = append(keys, skip...)
	for _, s := range swap {
		keys = append(keys, s.Key)
	}
	keys = append(keys, include...)
	for _, key := range keys {
		known := false
		for _, i := range items {
			known = known || matches(key, i)
		}
		if !known {
			names := make([]string, len(items))
			for n, i := range items {
				names[n] = i.Ingredient
			}
			return nil, nil, &PlanError{fmt.Sprintf("Opskriften har ikke %s. Ingredienser: %s.", pystr.Repr(key), strings.Join(names, ", "))}
		}
	}
	var lines []Line
	byID := map[string]int{}
	skipped := []Skipped{}
	for _, item := range items {
		product := item.Product
		reason := ""
		if anyMatch(skip, item) {
			reason = "fravalgt"
		} else if item.HaveAtHome && !anyMatch(include, item) {
			reason = "har du (butikken regner med det)"
		}
		newID := ""
		for _, s := range swap {
			if matches(s.Key, item) {
				newID = s.Value // the first key that fits; an empty value swaps nothing
				break
			}
		}
		if newID != "" {
			found := false
			for _, a := range item.Alternatives {
				if a.ID == newID {
					product, found = a, true
					break
				}
			}
			if !found {
				ids := make([]string, len(item.Alternatives))
				for n, a := range item.Alternatives {
					ids[n] = a.ID + " " + a.Name
				}
				list := strings.Join(ids, ", ")
				if list == "" {
					list = "ingen"
				}
				return nil, nil, &PlanError{fmt.Sprintf("%s er ikke et alternativ til %s: %s.", newID, item.Ingredient, list)}
			}
		}
		if reason == "" && !product.Available {
			reason = "udsolgt"
		}
		if reason != "" {
			skipped = append(skipped, Skipped{Ingredient: item.Ingredient, Name: product.Name, Reason: reason})
			continue
		}
		n, seen := byID[product.ID]
		if !seen {
			n = len(lines)
			byID[product.ID] = n
			lines = append(lines, Line{ProductID: product.ID, Ingredients: []string{}, Name: product.Name})
		}
		lines[n].Ingredients = append(lines[n].Ingredients, item.Ingredient)
		lines[n].Quantity += product.Quantity
	}
	return lines, skipped, nil
}

// InCart is product id → quantity in the cart now.
func InCart(cart model.Cart) map[string]int {
	out := map[string]int{}
	for _, ln := range cart.Lines {
		out[ln.ProductID] = ln.Quantity
	}
	return out
}

// ReportLine is before, added and after for one product.
type ReportLine struct {
	ProductID   string   `json:"product_id"`
	Name        string   `json:"name"`
	Ingredients []string `json:"ingredients"`
	Before      int      `json:"before"`
	Added       int      `json:"added"`
	After       *int     `json:"after"`
	OK          bool     `json:"ok"`
	Message     *string  `json:"message"`
}

// Report: before, added and after per product, from the store's SetQuantities results.
func Report(lines []Line, before map[string]int, results []model.QuantityResult) []ReportLine {
	byID := map[string]model.QuantityResult{}
	for _, r := range results {
		byID[r.ProductID] = r // a later result replaces an earlier one
	}
	out := make([]ReportLine, 0, len(lines))
	for _, ln := range lines {
		r := byID[ln.ProductID]
		out = append(out, ReportLine{
			ProductID: ln.ProductID, Name: ln.Name, Ingredients: ln.Ingredients,
			Before: before[ln.ProductID], Added: ln.Quantity,
			After: r.Applied, OK: r.OK, Message: r.Message,
		})
	}
	return out
}
