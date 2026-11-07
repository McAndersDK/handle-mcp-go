package bilkatogo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

var htmlTag = regexp.MustCompile(`<[^>]+>`)

// GetCart is what is in the cart now.
func (s *Store) GetCart(ctx context.Context) (_ model.Cart, err error) {
	defer guard(&err)
	data, err := s.cartJSON(ctx)
	if err != nil {
		return model.Cart{}, err
	}
	return s.cartOf(data), nil
}

// cartOf is _cart(). The fees are cart groups that are not products; the address is read only for
// the few facts we need (delivery.go), never as a whole.
func (s *Store) cartOf(data obj) model.Cart {
	stat, addr := objOrEmpty(data["stat"]), readAddress(data)
	lines := cartLines(data["lines"])
	fees := model.Dict[model.Money]{}
	for _, f := range [][2]string{{"levering", "delivery_price"}, {"pakkeservice", "packing"},
		{"emballage", "packaging_price"}, {"pant", "deposit"}} {
		if truthy(stat[f[1]]) {
			fees[f[0]] = kr(stat[f[1]])
		}
	}
	delivery := optStr(objOrEmpty(data["deliveryDate"])["delivery_message"])
	if s.slotEnded(data) {
		delivery = nil // the slot of a past order is no choice
	}
	warnings := model.List[string]{}
	if deliveryMode(addr) == modeUnknown {
		warnings = append(warnings, notKnown)
	}
	if text := stat["minimum_left_text"]; truthy(text) {
		warnings = append(warnings, htmlTag.ReplaceAllString(mustText(text), ""))
	}
	for _, m := range listOrEmpty(data["messages"]) {
		if d, ok := m.(map[string]any); ok {
			warnings = append(warnings, textOf(withDefault(d, "text", pyRepr(d))))
		} else {
			warnings = append(warnings, pyStr(m))
		}
	}
	total := model.Money{}
	for _, ln := range lines {
		total = total.Add(ln.LineTotal)
	}
	met := !truthy(stat["minimum_left"])
	cart := model.Cart{
		Store: name, Lines: lines, ProductsTotal: total, Fees: fees, Total: kr(stat["price"]),
		MinimumMet: &met, Delivery: delivery, Warnings: warnings,
		DeliveryMode: modeLabel(deliveryMode(addr)), PickupStore: storeOf(addr),
	}
	if truthy(addr.minimumBuy) {
		m := kr(addr.minimumBuy)
		cart.MinimumOrder = &m
	}
	return cart
}

// SetQuantities sets absolute quantities, in the order asked; 0 removes a line. It never orders
// or pays. Bilka's own refusal of one product is reported for that product and the rest goes on;
// any other failure stops the loop. This is the only call in the package that changes anything.
func (s *Store) SetQuantities(ctx context.Context, items []store.CartItem) (_ []model.QuantityResult, err error) {
	defer guard(&err)
	results := []model.QuantityResult{}
	for _, it := range items {
		params := pairs{{"productId", it.ProductID}, {"count", strconv.Itoa(it.Quantity)}, {"fullCart", "0"}}
		answer, err := s.shop(ctx, "GET", "shop/v6/ChangeLineCount", params)
		var rf *refused
		if errors.As(err, &rf) {
			results = append(results, model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity, Message: strPtr(rf.Error())})
			continue
		} else if err != nil {
			return nil, err
		}
		applied := answer["productAmount"]
		var notes []string
		for _, key := range []string{"message", "offerLimitMessage"} {
			if v := answer[key]; truthy(v) {
				notes = append(notes, mustText(v))
			}
		}
		note := strings.Join(notes, " ")
		r := model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity, OK: isNum(applied, float64(it.Quantity))}
		if _, isBool := applied.(bool); isPyInt(applied) && !isBool {
			n := toInt(applied)
			r.Applied = &n
		}
		if !r.OK && note == "" {
			note = fmt.Sprintf("BilkaToGo satte antallet til %s, ikke %d.", pyStr(applied), it.Quantity)
		}
		if note != "" {
			r.Message = &note
		}
		results = append(results, r)
	}
	return results, nil
}
