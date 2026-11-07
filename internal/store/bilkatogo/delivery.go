package bilkatogo

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
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
	pickupStore   string // the store's name; set only in the pickup case, empty otherwise (docs/adr/0006)
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
	// `name` is the customer's name on a home cart, so it is read only when nothing says home and
	// it reads as a store. A home cart with its markers blanked stays unknown, and the name is dropped.
	if !out.home && out.sid != "" {
		if n, ok := a["name"].(string); ok && strings.HasPrefix(strings.TrimSpace(n), "Bilka") {
			out.pickupStore = strings.TrimSpace(n)
		}
	}
	return out
}

// deliveryMode is the one place that decides, and it fails closed (docs/adr/0006): home needs a
// DAWA id or a recipient; pickup needs a store id, no home marker and a store name that starts
// with "Bilka"; anything else is unknown.
func deliveryMode(a address) mode {
	switch {
	case a.home:
		return modeHome
	case a.sid != "" && a.pickupStore != "":
		return modePickup
	}
	return modeUnknown
}

// modeLabel is the model's delivery_mode: "home", "pickup" or nil.
func modeLabel(m mode) *string {
	switch m {
	case modeHome:
		return strPtr(model.DeliveryHome)
	case modePickup:
		return strPtr(model.DeliveryPickup)
	}
	return nil
}

// storeOf is the model's pickup_store: the store's name in the pickup case, nil otherwise.
func storeOf(a address) *string {
	if deliveryMode(a) != modePickup {
		return nil
	}
	return strPtr(a.pickupStore)
}

// pickupFee is the pickup cart's delivery_price, 0 included: Bilka says "0" for pickup and leaves
// the key out when the cart cannot tell. nil means unknown.
func pickupFee(data obj) *model.Money {
	v := objOrEmpty(data["stat"])["delivery_price"]
	if v == nil {
		return nil
	}
	fee := kr(v)
	return &fee
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
	t.DeliveryMode, t.PickupStore = modeLabel(deliveryMode(addr)), storeOf(addr)
	t.OtherFees = model.Dict[model.Money]{}
	for k, v := range cart.Fees {
		if k == "levering" {
			fee := v
			t.DeliveryFee = &fee
		} else {
			t.OtherFees[k] = v
		}
	}
	if deliveryMode(addr) == modePickup {
		t.DeliveryFee = pickupFee(data) // a pickup fee of 0 is known, not missing
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
	info := model.DeliveryInfo{Store: name, DeliveryMode: modeLabel(deliveryMode(addr)), PickupStore: storeOf(addr)}
	info.ChosenStart, info.ChosenEnd = s.chosenSlot(data)
	if truthy(addr.nextAvailable) {
		d, err := model.ParseDate(mustText(addr.nextAvailable))
		if err != nil {
			bad("nextAvailableDeliveryDate is not a date")
		}
		info.NextAvailable = &d
	}
	if deliveryMode(addr) == modeUnknown {
		info.Note = strPtr(notKnown)
	}
	return info, nil
}

// DeliverySlots is the list of delivery or pickup times for the cart's address (GET). Bilka has no
// price per slot: the fee is the cart's flat delivery price. Never choose a slot, a store or a
// mode here (docs/adr/0001, docs/adr/0006).
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
	addr := readAddress(cartData)
	md := deliveryMode(addr)
	if md == modeUnknown {
		return nil, store.Errorf("%s", notKnown)
	}
	var fee *model.Money
	if md == modePickup {
		fee = pickupFee(cartData)
	} else if f, ok := s.cartOf(cartData).Fees["levering"]; ok {
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
			if md == modePickup {
				sl.Kind, sl.PickupStore = strPtr("Afhentning"), storeOf(addr)
			} else if times.a.h < 6 {
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
