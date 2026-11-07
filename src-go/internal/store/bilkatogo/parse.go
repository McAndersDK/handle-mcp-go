package bilkatogo

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Danish time must work without a zoneinfo folder on the machine

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

var dk = mustLoad("Europe/Copenhagen")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

var hundred = model.NewMoney(100, 0)

// kr is _kr(): money from Bilka is in øre. `ore or 0`: a missing amount is 0, as in Python. An
// amount that is not a decimal number is a clear StoreError.
func kr(v any) model.Money {
	text := "0"
	if truthy(v) {
		text = pyStr(v)
	}
	m, err := model.ParseMoney(text)
	if err != nil {
		raise(store.Errorf("BilkaToGo sendte et ukendt beløb: %s.", pyRepr(v)))
	}
	q, err := m.Div(hundred)
	if err != nil { // dividing by 100 cannot fail
		raise(store.Errorf("BilkaToGo sendte et ukendt beløb: %s.", pyRepr(v)))
	}
	return q
}

// requiredKr is a price that must be there: a missing or null one is a format change, never
// 0.00 kr (ADR 0005). An explicit 0 is still 0.00 kr.
func requiredKr(v any, what string) model.Money {
	if v == nil {
		bad("no %s", what)
	}
	return kr(v)
}

// campaign is _campaign(): "Skarp pris" stays, "Mix 10 for 25,00" is "Mix", leaflet offers are
// "Tilbud".
func campaign(offer *string) *string {
	switch {
	case offer == nil:
		return nil
	case *offer == "Skarp pris":
		return offer
	case strings.HasPrefix(*offer, "Mix "):
		return strPtr("Mix")
	}
	return strPtr("Tilbud")
}

// fmt2 is Python's format(Decimal, ".2f"): rounds half to even, unlike the answers (half up).
func fmt2(m model.Money) string {
	t := m.MulMoney(hundred) // øre, maybe with a fraction
	ceil := model.MoneyFromBig(t.Ceil())
	floor := ceil
	if t.Cmp(ceil) != 0 {
		floor = ceil.Sub(model.NewMoney(1, 0))
	}
	var whole model.Money
	switch frac := t.Sub(floor); frac.Cmp(model.NewMoney(5, -1)) {
	case -1:
		whole = floor
	case 1:
		whole = ceil
	default: // exactly half: the even øre
		whole = floor
		if !isEven(floor) {
			whole = ceil
		}
	}
	q, _ := whole.Div(hundred)
	return q.String()
}

func isEven(whole model.Money) bool {
	half, _ := whole.Div(model.NewMoney(2, 0))
	return half.Cmp(half.Round(0)) == 0
}

// --- time -------------------------------------------------------------------

// combine is datetime.combine(day, time, TZ): the wall clock in Copenhagen. Where a time exists
// twice (the hour clocks go back) Python takes the first, and so do we. Where it never happened
// (the hour clocks go forward) Go gives the same moment in the new offset, where Python wrote
// 02:xx+01:00: a named exception in the golden test.
func combine(day model.Date, h, mi, s, ns int) time.Time {
	y, m, d := day.YMD()
	w := time.Date(y, m, d, h, mi, s, ns, dk)
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

// dateOf is date.fromisoformat(v or ""); false for anything that is not a date text.
func dateOf(v any) (model.Date, bool) {
	s := ""
	if truthy(v) {
		var ok bool
		if s, ok = v.(string); !ok {
			return model.Date{}, false // TypeError in Python
		}
	}
	d, err := model.ParseDate(s)
	return d, err == nil
}

type clock struct{ h, m, s, ns int }

// clockOf is time.fromisoformat(v); false for anything that is not a time text.
func clockOf(v any) (clock, bool) {
	s, ok := v.(string)
	if !ok {
		return clock{}, false
	}
	t, err := model.ParseDateTime("2000-01-01T" + s)
	if err != nil {
		return clock{}, false
	}
	tm := t.Time()
	return clock{tm.Hour(), tm.Minute(), tm.Second(), tm.Nanosecond()}, true
}

// slotTimes is what _slot() returns: a day and two clock times. Python compares two datetimes in
// the same zone by their wall clock, not by the moment they happen (they differ only on the two
// days a year the clocks change), so the comparisons below use the wall clock too.
type slotTimes struct {
	day  model.Date
	a, b clock
}

func (t slotTimes) start() time.Time { return combine(t.day, t.a.h, t.a.m, t.a.s, t.a.ns) }
func (t slotTimes) end() time.Time   { return combine(t.day, t.b.h, t.b.m, t.b.s, t.b.ns) }

// endAfterStart is `end > start`.
func (t slotTimes) endAfterStart() bool {
	x, y := [4]int{t.a.h, t.a.m, t.a.s, t.a.ns}, [4]int{t.b.h, t.b.m, t.b.s, t.b.ns}
	for i := range x {
		if x[i] != y[i] {
			return y[i] > x[i]
		}
	}
	return false
}

// endNextDay is `end + timedelta(days=1)`: the same wall clock, one day on.
func (t slotTimes) endNextDay() time.Time {
	y, m, d := t.day.YMD()
	n := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC) // carries over the end of a month
	next, _ := model.NewDate(n.Year(), n.Month(), n.Day())
	return combine(next, t.b.h, t.b.m, t.b.s, t.b.ns)
}

// endedBefore is `end < now`, by the wall clock in Copenhagen.
func (t slotTimes) endedBefore(now time.Time) bool {
	n := now.In(dk)
	y, m, d := t.day.YMD()
	end := time.Date(y, m, d, t.b.h, t.b.m, t.b.s, t.b.ns, time.UTC)
	wall := time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), time.UTC)
	return end.Before(wall)
}

// slot is _slot(): both times of a day, or false when the day or a time cannot be read.
func slot(day, start, end any) (slotTimes, bool) {
	d, ok := dateOf(day)
	if !ok {
		return slotTimes{}, false
	}
	a, ok := clockOf(start)
	if !ok {
		return slotTimes{}, false
	}
	b, ok := clockOf(end)
	if !ok {
		return slotTimes{}, false
	}
	return slotTimes{d, a, b}, true
}

// chosen is the delivery time in the cart: deliveryDate with its two clock times.
func chosen(data obj) (slotTimes, bool) {
	c := objOrEmpty(data["deliveryDate"])
	return slot(c["deliveryDate"], c["intervalStart"], c["intervalEnd"])
}

// slotEnded is _slot_ended(): Bilka keeps the slot of the last order in the cart ("Ons. 04/09"
// seen weeks later). True only when the slot parses and has ended; an unreadable slot is kept.
func (s *Store) slotEnded(data obj) bool {
	t, ok := chosen(data)
	return ok && t.endedBefore(s.now())
}

// chosenSlot is _chosen_slot(): the delivery time chosen in the cart; none when it has ended.
func (s *Store) chosenSlot(data obj) (start, end *model.DateTime) {
	if s.slotEnded(data) {
		return nil, nil
	}
	t, ok := chosen(data)
	if !ok {
		return nil, nil
	}
	x, y := model.NewDateTime(t.start()), model.NewDateTime(t.end())
	return &x, &y
}

// --- cart lines ---------------------------------------------------------------

// isFeeGroup: the cart groups "Emballage" and "Services" are fees, not products.
func isFeeGroup(headline any) bool {
	h, ok := headline.(string)
	return ok && (h == "Emballage" || h == "Services")
}

// cartLines is _lines(): cart and order lines are nested, groups, blocks, orderlines.
func cartLines(groups any) model.List[model.CartLine] {
	out := model.List[model.CartLine]{}
	for _, g := range listOrEmpty(groups) {
		group := asObj(g)
		if isFeeGroup(group["headline"]) {
			continue
		}
		for _, b := range listOrEmpty(group["lines"]) {
			for _, l := range listOrEmpty(asObj(b)["orderlines"]) {
				ln := asObj(l)
				product := objOrEmpty(ln["product"])
				out = append(out, model.CartLine{
					ProductID: pyStr(withDefault(product, "objectID", "")),
					Name:      textOf(withDefault(product, "name", "")),
					Quantity:  toIntOrZero(ln["quantity"]),
					LineTotal: kr(ln["amount"]),
				})
			}
		}
	}
	return out
}

// pyJSONString is json.dumps(s) with Python's defaults: ASCII only, escapes for the rest.
func pyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20 || (r >= 0x7f && r < 0x10000): // everything outside " " to "~"
			fmt.Fprintf(&b, `\u%04x`, r)
		case r >= 0x10000:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
