package nemlig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// defaultBagFee: bags for a normal week (~800 kr), seen live 2026-11. The basket only says the bag
// price when it has products, so compare_basket uses this for an empty cart.
var defaultBagFee = model.NewMoney(1785, -2)

// bagFee is HANDLE_NEMLIG_BAG_FEE, which overrides the estimate. A bad value logs and falls back.
func bagFee() model.Money {
	raw := os.Getenv("HANDLE_NEMLIG_BAG_FEE")
	if raw == "" {
		return defaultBagFee
	}
	fee, err := model.ParseMoney(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."))
	if err != nil {
		slog.Warn("HANDLE_NEMLIG_BAG_FEE er ikke et tal. Bruger standarden.", "value", raw, "default", defaultBagFee.String())
		return defaultBagFee
	}
	return fee
}

// basket is the logged-in basket. A saved cookie that dies mid-run gives an anonymous basket,
// not a 401: stop there.
func (s *Store) basket(ctx context.Context) (obj, error) {
	data, err := s.api(ctx, call{method: "GET", url: base + "/webapi/basket/GetBasket"})
	if err != nil {
		return nil, err
	}
	b := objOrEmpty(data)
	if s.isResumed() && !truthy(b["Email"]) {
		return nil, store.Expired(name)
	}
	return b, nil
}

// GetCart is what is in the basket now.
func (s *Store) GetCart(ctx context.Context) (_ model.Cart, err error) {
	defer guard(&err)
	b, err := s.basket(ctx)
	if err != nil {
		return model.Cart{}, err
	}
	return cartOf(b), nil
}

func cartOf(b obj) model.Cart {
	lines := model.List[model.CartLine]{}
	for _, l := range listOrEmpty(b["Lines"]) {
		ln := asObj(l)
		if toIntOrZero(ln["Quantity"]) <= 0 { // removed lines can stay with a price
			continue
		}
		lines = append(lines, model.CartLine{
			ProductID: pyStr(field(ln, "Id")),
			Name:      textOf(withDefault(ln, "Name", "")),
			Quantity:  toInt(withDefault(ln, "Quantity", json0)),
			LineTotal: moneyOrZero(ln["Price"]),
		})
	}
	products := money(b["TotalProductsPrice"])
	if products == nil {
		sum := model.Money{}
		for _, ln := range lines {
			sum = sum.Add(ln.LineTotal)
		}
		products = &sum
	}
	fees := model.Dict[model.Money]{}
	sum := *products
	for _, f := range [][2]string{{"levering", "DeliveryPrice"}, {"poser", "TotalBagsPrice"}, {"pant", "TotalDepositsPrice"}} {
		if truthy(b[f[1]]) {
			fee := moneyOrZero(b[f[1]])
			fees[f[0]] = fee
			sum = sum.Add(fee)
		}
	}
	total := money(b["TotalPrice"])
	if total == nil {
		total = &sum
	}
	warnings := model.List[string]{}
	for _, w := range listOrEmpty(b["ValidationFailures"]) {
		if m, ok := w.(obj); ok {
			warnings = append(warnings, textOf(withDefault(m, "Message", pyStr(w))))
		} else {
			warnings = append(warnings, pyStr(w))
		}
	}
	cart := model.Cart{
		Store: name, Lines: lines, ProductsTotal: *products, Fees: fees, Total: *total,
		MinimumOrder: money(b["MinimumOrderTotal"]), Delivery: optStr(b["FormattedDeliveryTime"]),
		Warnings: warnings, DeliveryMode: strPtr(model.DeliveryHome),
	}
	if v := b["IsMinTotalValid"]; v != nil {
		met := truthy(v)
		cart.MinimumMet = &met
	}
	return cart
}

// SetQuantities sets absolute quantities, in the order asked; 0 removes a line. It never orders
// or pays. One failure does not stop the rest.
func (s *Store) SetQuantities(ctx context.Context, items []store.CartItem) (_ []model.QuantityResult, err error) {
	defer guard(&err)
	if s.isResumed() {
		if _, err := s.basket(ctx); err != nil { // never fill an anonymous basket
			return nil, err
		}
	}
	results := []model.QuantityResult{}
	for _, it := range items {
		body := obj{
			"ProductId": it.ProductID, "quantity": it.Quantity,
			"AffectPartialQuantity": false, "disableQuantityValidation": false,
		}
		data, err := s.api(ctx, call{method: "POST", url: base + "/webapi/basket/AddToBasket", body: body})
		var se *store.StoreError
		if errors.As(err, &se) {
			msg := se.Msg
			results = append(results, model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity, Message: &msg})
			continue
		} else if err != nil {
			return nil, err
		}
		applied := 0
		for _, l := range listOrEmpty(objOrEmpty(data)["Lines"]) {
			ln := asObj(l)
			if pyStr(ln["Id"]) == it.ProductID {
				applied = toInt(withDefault(ln, "Quantity", json0))
				break
			}
		}
		r := model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity, Applied: &applied, OK: applied == it.Quantity}
		if !r.OK {
			r.Message = strPtr(fmt.Sprintf("nemlig satte antallet til %d, ikke %d.", applied, it.Quantity))
		}
		results = append(results, r)
	}
	return results, nil
}

// Terms are the delivery fee, bag fee and minimum order, from the basket.
func (s *Store) Terms(ctx context.Context) (_ model.StoreTerms, err error) {
	defer guard(&err)
	b, err := s.basket(ctx)
	if err != nil {
		return model.StoreTerms{}, err
	}
	empty := true
	for _, l := range listOrEmpty(b["Lines"]) {
		if toIntOrZero(asObj(l)["Quantity"]) > 0 {
			empty = false
		}
	}
	t := model.NewStoreTerms(name, true)
	t.DeliveryMode = strPtr(model.DeliveryHome)
	t.DeliveryFee = money(b["DeliveryPrice"])
	t.MinimumOrder = money(b["MinimumOrderTotal"])
	t.OtherFees = model.Dict[model.Money]{}
	if bags := money(b["TotalBagsPrice"]); empty {
		t.OtherFees["poser (anslået)"] = bagFee()
	} else if bags != nil && !bags.IsZero() {
		t.OtherFees["poser"] = *bags
	}
	return t, nil
}
