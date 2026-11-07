package bilkatogo

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// mode is what the cart's delivery address says about how the groceries arrive.
type mode int

const (
	modeUnknown mode = iota // no address, or one we cannot tell
	modeHome                // home delivery
	modePickup              // pickup in a store
)

// address is the few facts we read from the cart's deliveryAddress. The whole block holds the
// customer's name, street and coordinates, so it is never kept or passed on as it is.
type address struct {
	home          bool // a DAWA id or a recipient: home addresses have one, pickup points do not
	sid           string
	minimumBuy    any
	nextAvailable any
}

func readAddress(data obj) address {
	a := objOrEmpty(data["deliveryAddress"])
	out := address{
		home:          truthy(a["dawa_uid"]) || truthy(a["recipient_name"]),
		minimumBuy:    a["minimum_buy"],
		nextAvailable: a["nextAvailableDeliveryDate"],
	}
	if truthy(a["sid"]) {
		out.sid = pyStr(a["sid"])
	}
	return out
}

// deliveryMode is the one place that decides. Today only home is told apart from the rest: every
// mode but home gives NOT_HOME, as Python did (_is_home). The pickup rule (a store id and no home
// marker) is provisional and nothing depends on it yet.
func deliveryMode(a address) mode {
	switch {
	case a.home:
		return modeHome
	case a.sid != "":
		return modePickup
	}
	return modeUnknown
}

// Terms are the delivery fee, other fees and minimum order, from the cart.
func (s *Store) Terms(ctx context.Context) (_ model.StoreTerms, err error) {
	defer guard(&err)
	data, err := s.cartJSON(ctx)
	if err != nil {
		return model.StoreTerms{}, err
	}
	cart, addr := s.cartOf(data), readAddress(data)
	t := model.NewStoreTerms(name, deliveryMode(addr) == modeHome)
	t.OtherFees = model.Dict[model.Money]{}
	for k, v := range cart.Fees {
		if k == "levering" {
			fee := v
			t.DeliveryFee = &fee
		} else {
			t.OtherFees[k] = v
		}
	}
	t.MinimumOrder = cart.MinimumOrder
	return t, nil
}

// DeliveryInfo is the delivery time chosen in the cart. Read only.
func (s *Store) DeliveryInfo(ctx context.Context) (_ model.DeliveryInfo, err error) {
	defer guard(&err)
	data, err := s.cartJSON(ctx)
	if err != nil {
		return model.DeliveryInfo{}, err
	}
	addr := readAddress(data)
	info := model.DeliveryInfo{Store: name}
	info.ChosenStart, info.ChosenEnd = s.chosenSlot(data)
	if truthy(addr.nextAvailable) {
		d, err := model.ParseDate(mustText(addr.nextAvailable))
		if err != nil {
			bad("nextAvailableDeliveryDate is not a date")
		}
		info.NextAvailable = &d
	}
	if deliveryMode(addr) != modeHome {
		info.Note = strPtr(notHome)
	}
	return info, nil
}

// DeliverySlots is the list of delivery times for the cart's address (GET). Bilka has no price
// per slot: the fee is the cart's flat delivery price. Never choose a slot here (docs/adr/0001).
func (s *Store) DeliverySlots(ctx context.Context, days int) (_ []model.DeliverySlot, err error) {
	defer guard(&err)
	// Python asked for both at once; so do we. Both are waited for, so a failure of one leaves
	// no call running behind our back.
	var data, cartData obj
	var errSlots, errCart error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer guard(&errSlots)
		data, errSlots = s.shop(ctx, "GET", "shop/v3/AvailableDeliveryDates", nil)
	}()
	go func() {
		defer wg.Done()
		defer guard(&errCart)
		cartData, errCart = s.cartJSON(ctx)
	}()
	wg.Wait()
	if errSlots != nil {
		return nil, errSlots
	}
	if errCart != nil {
		return nil, errCart
	}
	if deliveryMode(readAddress(cartData)) != modeHome {
		return nil, store.Errorf("%s", notHome)
	}
	var fee *model.Money
	if f, ok := s.cartOf(cartData).Fees["levering"]; ok {
		fee = &f
	}
	unknown := ""
	if fee == nil {
		unknown = "Gebyret er ukendt: kurven viser det ikke (tom kurv?). "
	}
	// `days` calendar days, today included
	now := s.now().In(dk)
	lastDay := time.Date(now.Year(), now.Month(), now.Day()+days-1, 0, 0, 0, 0, time.UTC)
	last, _ := model.NewDate(lastDay.Year(), lastDay.Month(), lastDay.Day())
	slots := []model.DeliverySlot{}
	for _, d := range listOrEmpty(data["availableIntervals"]) {
		day := asObj(d)
		for _, i := range listOrEmpty(day["intervals"]) {
			iv := asObj(i)
			times, ok := slot(day["date"], iv["intervalStart"], iv["intervalEnd"])
			if !ok || times.day.Compare(last) > 0 {
				continue
			}
			var infos []string
			for _, m := range listOrEmpty(iv["info"]) {
				infos = append(infos, mustText(withDefault(asObj(m), "message", "")))
			}
			end := times.end()
			if !times.endAfterStart() {
				end = times.endNextDay()
			}
			sl := model.NewDeliverySlot(name, model.NewDateTime(times.start()), model.NewDateTime(end), nil)
			if fee != nil {
				f := *fee
				sl.Price = &f
			}
			sl.Available = !truthy(iv["closed"]) && bit(iv["availability_bit"])&1 == 1
			if times.a.h < 6 {
				sl.Kind = strPtr("Nat")
			}
			if note := pystr.Strip(unknown + htmlTag.ReplaceAllString(strings.Join(infos, " "), "")); note != "" {
				sl.Note = &note
			}
			slots = append(slots, sl)
		}
	}
	return slots, nil
}

// bit is `iv.get("availability_bit") or 0` for `1 & x`: a whole number, or a bool.
func bit(v any) int {
	if !truthy(v) {
		return 0
	}
	if !isPyInt(v) {
		bad("availability_bit is not a whole number")
	}
	return toInt(v)
}
