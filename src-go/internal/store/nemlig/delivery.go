package nemlig

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// DeliveryInfo is the delivery time chosen in the basket. Read only.
func (s *Store) DeliveryInfo(ctx context.Context) (_ model.DeliveryInfo, err error) {
	defer guard(&err)
	b, err := s.basket(ctx)
	if err != nil {
		return model.DeliveryInfo{}, err
	}
	data, err := s.api(ctx, call{method: "GET", url: base + "/webapi/Order/DeliverySpot"})
	if err != nil {
		return model.DeliveryInfo{}, err
	}
	spot := objOrEmpty(data)
	slot := objOrEmpty(spot["TimeSlot"])
	info := model.DeliveryInfo{
		Store:        name,
		ChosenStart:  timeVal(slot["Start"]),
		ChosenEnd:    timeVal(slot["End"]),
		EditDeadline: timeVal(spot["EditDeadline"]),
		DeliveryMode: strPtr(model.DeliveryHome),
	}
	// nemlig keeps the slot of the last order in the basket. A slot that has ended is no chosen
	// slot: compare would price offers on a past day (docs/adr/0005). Unreadable is kept.
	if s.slotEnded(info.ChosenEnd) {
		info.ChosenStart, info.ChosenEnd, info.EditDeadline = nil, nil, nil
		b = obj{}
	}
	if chosen := b["FormattedDeliveryTime"]; truthy(chosen) {
		info.Note = strPtr(fmt.Sprintf("Valgt tid i kurven: %s. Gebyret afhænger af tiden (ca. 19-51 kr.).", pyStr(chosen)))
	} else {
		info.Note = strPtr("Ingen tid valgt. Gebyret afhænger af tiden (ca. 19-51 kr.). Vælg på nemlig.com.")
	}
	return info, nil
}

// slotEnded is true when the slot's end is before now. A time with a zone is an instant; one
// without is the Danish wall clock. No end (unreadable or missing) is not ended.
func (s *Store) slotEnded(end *model.DateTime) bool {
	if end == nil {
		return false
	}
	if end.Aware() {
		return end.Time().Before(s.now())
	}
	n := s.now().In(dk)
	wall := time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), time.UTC)
	return end.Time().Before(wall)
}

// DeliverySlots is the time picker's list (GET), logged in so prices are for our zone. Never
// choose a slot here: the human does (docs/adr/0001).
func (s *Store) DeliverySlots(ctx context.Context, days int) (_ []model.DeliverySlot, err error) {
	defer guard(&err)
	slots := []model.DeliverySlot{}
	var start any
	seen := 0
	for range days { // every page has at least one day, so this is the cap
		p := params{}.add("days", days-seen).add("showForSubscriptions", "false")
		if truthy(start) {
			p = p.add("startDate", start)
		}
		data, err := s.api(ctx, call{method: "GET", url: base + "/webapi/v2/Delivery/GetDeliveryDays", params: p})
		if err != nil {
			return nil, err
		}
		page := objOrEmpty(data)
		dayList := listOrEmpty(page["DayRangeHours"])
		for _, d := range dayList {
			for _, h := range listOrEmpty(asObj(d)["DayHours"]) {
				if slot, ok := slotOf(asObj(h)); ok {
					slots = append(slots, slot)
				}
			}
		}
		seen, start = seen+len(dayList), page["NextRangeStart"]
		if len(dayList) == 0 || !truthy(start) || seen >= days {
			break
		}
	}
	return slots, nil
}

// wall is the Danish wall-clock time day + hours, built with time.Date so daylight saving is
// right. Python added a timedelta to a wall-clock date. Where that gave a time that does not
// exist (02:00 on the day clocks go forward) Go gives the same moment in the new offset; where a
// time exists twice (02:00 when clocks go back) both pick the first, as Python did.
func wall(day model.DateTime, hours float64) time.Time {
	t := day.Time()
	y, m, d := t.Date()
	secs := t.Hour()*3600 + t.Minute()*60 + t.Second() + int(math.Round(hours*3600))
	w := time.Date(y, m, d, 0, 0, secs, t.Nanosecond(), dk)
	if earlier := w.Add(-time.Hour); sameWall(earlier, w) {
		return earlier
	}
	return w
}

func sameWall(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd && a.Hour() == b.Hour() && a.Minute() == b.Minute() &&
		a.Second() == b.Second()
}

func hoursOf(v any) float64 {
	n, ok := v.(interface{ Float64() (float64, error) })
	if !ok {
		bad("hour is not a number")
	}
	f, err := n.Float64()
	if err != nil {
		bad("hour is not a number")
	}
	return f
}

// slotOf is _slot(): nil (false) for a slot with no date or hours.
func slotOf(h obj) (model.DeliverySlot, bool) {
	day, deadline := timeVal(h["Date"]), timeVal(h["Deadline"])
	if day == nil || h["StartHour"] == nil || h["EndHour"] == nil {
		return model.DeliverySlot{}, false
	}
	slot := model.NewDeliverySlot(name,
		model.NewDateTime(wall(*day, hoursOf(h["StartHour"]))),
		model.NewDateTime(wall(*day, hoursOf(h["EndHour"]))),
		money(h["DeliveryPrice"]))
	slot.Available = isNumber(h["Availability"], 0) // 1 past deadline, 2 sold out, 3 off
	switch {
	case isNumber(h["Type"], 0):
		slot.Kind = strPtr("Personlig") // attended delivery
	case isNumber(h["Type"], 1):
		slot.Kind = strPtr("Fleksibel") // unattended delivery
	}
	if deadline != nil {
		dt := deadline.Time()
		slot.Note = strPtr(fmt.Sprintf("Bestil senest %02d/%02d kl. %02d:%02d", dt.Day(), int(dt.Month()), dt.Hour(), dt.Minute()))
	}
	return slot, true
}

// isNumber is `v == n` for a JSON value.
func isNumber(v any, n float64) bool {
	num, ok := v.(interface{ Float64() (float64, error) })
	if !ok {
		return false
	}
	f, err := num.Float64()
	return err == nil && f == n
}

var orderStatus = map[float64]string{1: "Bestilt", 2: "Under behandling", 4: "Leveret"}

func orderOf(o obj, lines model.List[model.CartLine]) model.Order {
	delivery := objOrEmpty(o["DeliveryTime"])
	status := "status " + pyStr(o["Status"])
	if num, ok := o["Status"].(interface{ Float64() (float64, error) }); ok {
		if f, err := num.Float64(); err == nil {
			if text, known := orderStatus[f]; known {
				status = text
			}
		}
	}
	if lines == nil {
		lines = model.List[model.CartLine]{}
	}
	return model.Order{
		Store: name, ID: pyStr(withDefault(o, "Id", "")), Status: status, Total: moneyOrZero(o["Total"]),
		PlacedAt: timeVal(o["OrderDate"]), DeliveryStart: timeVal(delivery["Start"]),
		DeliveryEnd: timeVal(delivery["End"]), Lines: lines,
	}
}

// Orders are the past orders, newest first as nemlig sends them.
func (s *Store) Orders(ctx context.Context, limit int) (_ []model.Order, err error) {
	defer guard(&err)
	p := params{}.add("skip", 0).add("take", limit)
	data, err := s.api(ctx, call{method: "GET", url: base + "/webapi/order/GetBasicOrderHistory", params: p})
	if err != nil {
		return nil, err
	}
	out := []model.Order{}
	if !truthy(data) { // (data or {}).get("Orders", [])
		return out, nil
	}
	m := asObj(data)
	list, present := m["Orders"]
	if !present {
		return out, nil
	}
	l, ok := list.([]any)
	if !ok {
		bad("Orders is not a list")
	}
	for _, o := range l {
		out = append(out, orderOf(asObj(o), nil))
	}
	return out, nil
}

// Order is one order with its lines. The id goes into the URL path, so it must be digits.
func (s *Store) Order(ctx context.Context, orderID string) (_ model.Order, err error) {
	defer guard(&err)
	if !isOrderID(orderID) {
		return model.Order{}, store.Errorf("Ordrenummeret skal være tal, som i get_orders.")
	}
	raw, err := s.api(ctx, call{method: "GET", url: base + "/webapi/v2/order/GetOrderHistory/" + orderID})
	if err != nil {
		return model.Order{}, err
	}
	data := objOrEmpty(raw)
	lines := model.List[model.CartLine]{}
	for _, l := range listOrEmpty(data["Lines"]) {
		ln := asObj(l)
		lines = append(lines, model.CartLine{
			ProductID: pyStr(withDefault(ln, "ProductNumber", "")),
			Name:      textOf(withDefault(ln, "ProductName", "")),
			Quantity:  toInt(withDefault(ln, "Quantity", json0)),
			LineTotal: moneyOrZero(ln["Amount"]),
		})
	}
	if len(lines) == 0 && !truthy(data["Id"]) {
		return model.Order{}, store.Errorf("Fandt ikke ordre %s hos nemlig.", orderID)
	}
	meta := obj{"Id": orderID}
	for k, v := range data {
		if k != "Lines" {
			meta[k] = v
		}
	}
	order := orderOf(meta, lines)
	if order.Total.IsZero() {
		sum := model.Money{}
		for _, ln := range lines {
			sum = sum.Add(ln.LineTotal)
		}
		order.Total = sum
	}
	return order, nil
}

// isOrderID is re.fullmatch("[0-9]{1,20}").
func isOrderID(id string) bool {
	if len(id) < 1 || len(id) > 20 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}
