package bilkatogo

import (
	"context"
	"strconv"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// status maps Bilka's codes to the words the website shows.
var status = map[string]string{
	"Bestilt":    "Modtaget",
	"Ubehandlet": "Under behandling",
	"Pakket":     "Pakket",
	"Leveret":    "Leveret",
	"Faktureret": "Leveret",
	"Annulleret": "Annulleret",
}

// orderOf is _order().
func orderOf(o obj, lines model.List[model.CartLine]) model.Order {
	interval := objOrEmpty(o["deliveryInterval"])
	order := model.Order{Store: name, ID: pyStr(withDefault(o, "orderId", "")), Lines: lines}
	if lines == nil {
		order.Lines = model.List[model.CartLine]{}
	}
	if st, ok := o["status"].(string); ok && status[st] != "" {
		order.Status = status[st]
	} else if truthy(o["status"]) {
		order.Status = pyStr(o["status"])
	} else {
		order.Status = "ukendt"
	}
	// total: packedPrice, else orderedPrice, else price (the first one that is not 0 or missing)
	var total any
	for _, key := range []string{"packedPrice", "orderedPrice", "price"} {
		if total = o[key]; truthy(total) {
			break
		}
	}
	order.Total = kr(total)
	if t, ok := slot(o["deliveryDate"], interval["start"], interval["end"]); ok {
		start, end := model.NewDateTime(t.start()), model.NewDateTime(t.end())
		order.DeliveryStart, order.DeliveryEnd = &start, &end
	}
	if ordered := objOrEmpty(o["ordered"]); truthy(ordered["date"]) {
		at := "00:00:00"
		if truthy(ordered["time"]) {
			at = pyStr(ordered["time"])
		}
		naive, err := model.ParseDateTime(pyStr(ordered["date"]) + "T" + at)
		if err != nil {
			bad("ordered is not a time")
		}
		t := naive.Time() // the wall clock as written, in Copenhagen
		d, _ := model.NewDate(t.Year(), t.Month(), t.Day())
		placed := model.NewDateTime(combine(d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond()))
		order.PlacedAt = &placed
	}
	return order
}

// Orders are the past orders. Bilka asks for them with a POST that only reads.
func (s *Store) Orders(ctx context.Context, limit int) (_ []model.Order, err error) {
	defer guard(&err)
	params := pairs{{"limit", strconv.Itoa(limit)}, {"offset", "0"}, {"muah", "1"}, {"ordertype", "dibs,pgw"}}
	data, err := s.shop(ctx, "POST", "shop/v7/OrderHistory", params)
	if err != nil {
		return nil, err
	}
	out := []model.Order{}
	for _, o := range listOrEmpty(data["orders"]) {
		out = append(out, orderOf(asObj(o), nil))
	}
	return out, nil
}

// Order is one order with its lines. The id is sent as a query value, whatever it holds.
func (s *Store) Order(ctx context.Context, orderID string) (_ model.Order, err error) {
	defer guard(&err)
	data, err := s.shop(ctx, "GET", "shop/v7/OrderDetails", pairs{{"orderId", orderID}})
	if err != nil {
		return model.Order{}, err
	}
	details := data["order"]
	if !truthy(details) {
		return model.Order{}, store.Errorf("Fandt ikke ordre %s hos BilkaToGo.", orderID)
	}
	d := asObj(details)
	return orderOf(d, cartLines(d["lines"])), nil
}
