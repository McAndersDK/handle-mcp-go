package server

import (
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/recipecart"
)

// The answers of the tools that are not a model on its own. Every key here is classified in
// privacy.PassthroughKeys; classify_test.go fails when a new one is not.

// compareAnswer is compare_basket: each store's quote (or its error) next to the verdict.
type compareAnswer struct {
	Stores map[string]any `json:"stores"`
	compare.VerdictAnswer
}

// setAnswer is set_cart_quantities: a result per product and the new cart.
type setAnswer struct {
	Results model.List[model.QuantityResult] `json:"results"`
	Cart    model.Cart                       `json:"cart"`
}

// addAnswer is add_recipe_to_cart: before, added and after per product, the left-out items and the new cart.
type addAnswer struct {
	Added   model.List[recipecart.ReportLine] `json:"added"`
	Skipped model.List[recipecart.Skipped]    `json:"skipped"`
	Cart    model.Cart                        `json:"cart"`
}

// deliveryAnswer is get_delivery_info.
type deliveryAnswer struct {
	Delivery model.DeliveryInfo `json:"delivery"`
	Terms    model.StoreTerms   `json:"terms"`
}

// slotsAnswer is one store in list_delivery_slots: the free slots in time order and the cheapest three.
type slotsAnswer struct {
	Cheapest model.List[model.DeliverySlot] `json:"cheapest"`
	Slots    model.List[model.DeliverySlot] `json:"slots"`
}

// ordersAnswer is get_orders.
type ordersAnswer struct {
	Orders model.List[model.Order] `json:"orders"`
}

// lst makes a nil slice from a store an empty list, never null.
func lst[T any](x []T) model.List[T] { return x }

func clamp(n, lo, hi int) int { return max(lo, min(n, hi)) }

// personsOf clamps persons to what the stores take; nil stays nil.
func personsOf(p *int) *int {
	if p == nil {
		return nil
	}
	n := clamp(*p, MinPersons, MaxPersons)
	return &n
}
