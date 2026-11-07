package rema1000

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// REMA 1000 is pickup only: the human picks up in the store chosen at login, and chooses the
// time and pays on rema1000.dk. Nothing here chooses a store, a time or a mode (docs/adr/0006,
// docs/adr/0008).
const (
	pickupKind = "Afhentning"
	// noFee is the reason for a missing fee: REMA's time slots carry none.
	noFee = "REMA 1000 oplyser ikke et gebyr for afhentning her."
	// infoNote is why there is no chosen time.
	infoNote = "Afhentningstid vælges og betales hos REMA 1000. handle-mcp vælger ingen tid. " +
		"REMA 1000 oplyser hverken gebyr eller mindstebeløb her."
)

// apiSlot is one pickup time. Times come in UTC.
type apiSlot struct {
	From        string `json:"from"`
	To          string `json:"to"`
	IsAvailable bool   `json:"is_available"`
}

// slots reads the pickup times of the chosen store, in time order, as Copenhagen times.
func (s *Store) slots(ctx context.Context, pickup *pickupStore) ([]model.DeliverySlot, error) {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	err := s.getJSON(ctx, call{method: "GET", path: fmt.Sprintf("v3/stores/%d/time-slots", pickup.ID), auth: true},
		"afhentningstiderne", &env)
	if err != nil {
		return nil, err
	}
	byDay := map[string][]apiSlot{}
	if d := bytes.TrimSpace(env.Data); len(d) > 0 && d[0] == '{' { // an empty answer is [], not {}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(d, &raw); err != nil {
			return nil, store.Errorf("REMA 1000 svarede med noget uventet (afhentningstiderne). API'et kan have ændret sig.")
		}
		for day, v := range raw {
			var list []apiSlot
			if json.Unmarshal(v, &list) == nil { // only the days: anything else in there is not a slot
				byDay[day] = list
			}
		}
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	var out []model.DeliverySlot
	for _, d := range days {
		for _, sl := range byDay[d] {
			from, e1 := time.Parse(time.RFC3339, sl.From)
			to, e2 := time.Parse(time.RFC3339, sl.To)
			if e1 != nil || e2 != nil {
				return nil, store.Errorf("REMA 1000 sendte en afhentningstid, der ikke er en tid. API'et kan have ændret sig.")
			}
			slot := model.NewDeliverySlot(name, model.NewDateTime(from.In(dk)), model.NewDateTime(to.In(dk)), nil)
			slot.Available = sl.IsAvailable
			slot.Kind, slot.PickupStore = strPtr(pickupKind), strPtr(pickup.Name)
			slot.Note = strPtr(noFee)
			out = append(out, slot)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Time().Before(out[j].Start.Time()) })
	return out, nil
}

// DeliverySlots lists pickup times for the next `days` days, today included, in the chosen store
// only. The fee is null: REMA does not say it here.
func (s *Store) DeliverySlots(ctx context.Context, days int) ([]model.DeliverySlot, error) {
	pickup, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.slots(ctx, pickup)
	if err != nil {
		return nil, err
	}
	now := s.now().In(dk)
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, dk)
	until := first.AddDate(0, 0, max(days, 1))
	out := []model.DeliverySlot{}
	for _, sl := range all {
		if t := sl.Start.Time(); !t.Before(first) && t.Before(until) {
			out = append(out, sl)
		}
	}
	return out, nil
}

// DeliveryInfo says pickup in the chosen store. No time is chosen here: that happens at payment.
func (s *Store) DeliveryInfo(ctx context.Context) (model.DeliveryInfo, error) {
	pickup, err := s.ready(ctx)
	if err != nil {
		return model.DeliveryInfo{}, err
	}
	all, err := s.slots(ctx, pickup)
	if err != nil {
		return model.DeliveryInfo{}, err
	}
	info := model.DeliveryInfo{Store: name, DeliveryMode: strPtr(model.DeliveryPickup),
		PickupStore: strPtr(pickup.Name), Note: strPtr(infoNote)}
	for _, sl := range all {
		if sl.Available {
			d := sl.Start.Date()
			info.NextAvailable = &d
			break
		}
	}
	return info, nil
}

// Terms: pickup, so no home delivery. REMA's API has no fee or minimum order that we know of, so
// they are null (infoNote says why in get_delivery_info).
func (s *Store) Terms(ctx context.Context) (model.StoreTerms, error) {
	pickup, err := s.ready(ctx)
	if err != nil {
		return model.StoreTerms{}, err
	}
	t := model.NewStoreTerms(name, false)
	t.OtherFees = model.Dict[model.Money]{}
	t.DeliveryMode, t.PickupStore = strPtr(model.DeliveryPickup), strPtr(pickup.Name)
	return t, nil
}

// Orders: no order endpoint is known, and ordering is the human's job anyway.
func (s *Store) Orders(context.Context, int) ([]model.Order, error) {
	return nil, store.ErrNotImplemented
}

// Order: see Orders.
func (s *Store) Order(context.Context, string) (model.Order, error) {
	return model.Order{}, store.ErrNotImplemented
}
