package rema1000

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// The basket is a shopping list called "handle-mcp 2026-11-07" (docs/adr/0008). It is the only
// list this adapter reads or writes. It never touches another list, deletes a list, changes which
// list is primary, shares one, or sets store, time or coupon.
const listPrefix = "handle-mcp "

// apiList is a list in the user's list of lists.
type apiList struct {
	ID        json.Number `json:"id"`
	Name      string      `json:"name"`
	IsPrimary bool        `json:"is_primary"`
}

// apiItem is one line on a list. A line written as free text has no store_item.
type apiItem struct {
	ID          json.Number `json:"id"`
	Amount      json.Number `json:"amount"`
	Bought      bool        `json:"bought"`
	Name        string      `json:"name"`
	StoreItemID json.Number `json:"store_item_id"`
	StoreItem   *struct {
		Name    string `json:"name"`
		Pricing struct {
			Price        json.Number `json:"price"`
			MaxQuantity  json.Number `json:"max_quantity"`
			PriceOverMax json.Number `json:"price_over_max"`
			Deposit      json.Number `json:"deposit"`
			OnDiscount   bool        `json:"is_on_discount"`
		} `json:"pricing"`
	} `json:"store_item"`
}

// apiListFull is one list read with its items. Here the flag is called "primary".
type apiListFull struct {
	ID      json.Number `json:"id"`
	Name    string      `json:"name"`
	Primary bool        `json:"primary"`
	Items   []apiItem   `json:"items"`
}

// listName is today's list, by the Copenhagen date.
func (s *Store) listName() string { return listPrefix + s.today() }

// findToday is today's list, or nil when there is none. Only the exact name counts.
func (s *Store) findToday(ctx context.Context) (*apiList, error) {
	uid, err := s.user()
	if err != nil {
		return nil, err
	}
	c := call{method: "GET", path: fmt.Sprintf("v3/users/%d/shopping-lists", uid), auth: true}
	lists, err := pages[apiList](ctx, s, c, "dine lister", 100, 10, nil)
	if err != nil {
		return nil, err
	}
	want := s.listName()
	for _, l := range lists {
		if l.Name == want {
			return &l, nil
		}
	}
	return nil, nil
}

// readList reads one list with its items. Never with=users: that brings names and emails.
func (s *Store) readList(ctx context.Context, id json.Number) (apiListFull, error) {
	var l apiListFull
	err := s.getJSON(ctx, call{method: "GET", path: "v2/shoppinglists/" + url.PathEscape(string(id)),
		query: url.Values{"with": {"items"}}, auth: true}, "indkøbslisten", &l)
	return l, err
}

// qty is an item's amount as a whole number.
func qty(n json.Number) int {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int(f + 0.5)
}

// lineTotal is what REMA's own list adds up: the price per unit, and past the offer's limit the
// price over the limit (which is the plain price when REMA gives none).
func lineTotal(it apiItem) (model.Money, model.Money, error) {
	pr := it.StoreItem.Pricing
	price, _, err := money(pr.Price)
	if err != nil {
		return model.Money{}, model.Money{}, err
	}
	n := qty(it.Amount)
	total := price.Mul(int64(n))
	if limit := qty(pr.MaxQuantity); pr.OnDiscount && limit > 0 && n > limit {
		over := price
		if m, has, _ := money(pr.PriceOverMax); has {
			over = m
		}
		total = price.Mul(int64(limit)).Add(over.Mul(int64(n - limit)))
	}
	deposit, _, err := money(pr.Deposit)
	if err != nil {
		return model.Money{}, model.Money{}, err
	}
	return total, deposit.Mul(int64(n)), nil
}

// GetCart reads today's list. Lines bought in the app are left out. No list for today is an empty
// cart that says which list it would use.
func (s *Store) GetCart(ctx context.Context) (model.Cart, error) {
	pickup, err := s.ready(ctx)
	if err != nil {
		return model.Cart{}, err
	}
	cart := model.Cart{Store: name, Lines: model.List[model.CartLine]{}, Fees: model.Dict[model.Money]{},
		Warnings: model.List[string]{}, DeliveryMode: strPtr(model.DeliveryPickup), PickupStore: strPtr(pickup.Name)}
	list, err := s.findToday(ctx)
	if err != nil {
		return model.Cart{}, err
	}
	if list == nil {
		cart.Warnings = append(cart.Warnings, fmt.Sprintf(
			"Ingen liste for i dag. Kurven bliver listen %q, når du lægger noget i den.", s.listName()))
		return cart, nil
	}
	full, err := s.readList(ctx, list.ID)
	if err != nil {
		return model.Cart{}, err
	}
	var deposits model.Money
	texts := 0
	for _, it := range full.Items {
		switch {
		case it.Bought:
			continue
		case it.StoreItem == nil || it.StoreItemID == "":
			texts++ // free text, no product and no price
			continue
		}
		total, deposit, err := lineTotal(it)
		if err != nil {
			return model.Cart{}, err
		}
		cart.Lines = append(cart.Lines, model.CartLine{ProductID: string(it.StoreItemID),
			Name: it.StoreItem.Name, Quantity: qty(it.Amount), LineTotal: total})
		cart.ProductsTotal = cart.ProductsTotal.Add(total)
		deposits = deposits.Add(deposit)
	}
	cart.Total = cart.ProductsTotal
	if deposits.Sign() > 0 {
		cart.Fees["pant"] = deposits
		cart.Total = cart.Total.Add(deposits)
	}
	cart.Warnings = append(cart.Warnings, fmt.Sprintf(
		"Kurven er listen %q i REMA 1000. Afhentningstid og betaling vælger du selv hos REMA 1000.", full.Name))
	if texts > 0 {
		cart.Warnings = append(cart.Warnings, fmt.Sprintf(
			"%d linjer på listen er skrevet som tekst uden pris og er ikke med i summen.", texts))
	}
	return cart, nil
}

func strPtr(s string) *string { return &s }
