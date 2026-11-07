package nemlig

// Product parsing for the search gateway (the old API's hits). Port of the helpers at the top of
// nemlig.py: _time, _unit, _offer, _discount_price, _until, _kr and _product.

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

// timeVal is _time(): an ISO time, or nil. nemlig sometimes sends text ("03/11-2026 kl. 14-21"),
// and 0001-/9999- years mean "no time".
func timeVal(v any) *model.DateTime {
	if !truthy(v) {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		bad("a time must be text")
	}
	if strings.HasPrefix(s, "0001-") || strings.HasPrefix(s, "9999-") {
		return nil
	}
	t, err := model.ParseDateTime(s)
	if err != nil {
		if !errors.Is(err, model.ErrNotATime) {
			bad("time")
		}
		slog.Debug("nemlig: not a time")
		return nil
	}
	return &t
}

// unitOf is _unit(): 'kr./Kg.' is 'kg', 'kr/l' is 'l', 'kr/stk' is 'stk'.
func unitOf(label any) *string {
	if !truthy(label) {
		return nil
	}
	s, ok := label.(string)
	if !ok {
		bad("a unit label must be text")
	}
	parts := strings.Split(strings.ReplaceAll(pystr.Lower(s), ".", ""), "/")
	tail := pystr.Strip(parts[len(parts)-1])
	switch tail {
	case "kg", "l", "stk":
		return strPtr(tail)
	case "ltr":
		return strPtr("l")
	case "":
		return nil
	}
	return strPtr(tail)
}

// kr is _kr(): Danish short form, "13,95" or "68". Python's format() rounds half to even.
func kr(m model.Money) string {
	d, err := decimal.NewFromString(m.FormatG())
	if err != nil { // a value this large is not a price
		return m.String()
	}
	s := strings.ReplaceAll(d.RoundBank(2).StringFixed(2), ".", ",")
	return strings.TrimSuffix(s, ",00")
}

// number reads a JSON number for a comparison, as Python compares it. Text is a TypeError there.
func number(v any) model.Money {
	m := money(v)
	if m == nil {
		return model.Money{}
	}
	if _, isText := v.(string); isText {
		bad("a number was text")
	}
	return *m
}

var one = model.NewMoney(1, 0)

// discountPrice is _discount_price(): what you pay for a plain discount. Multi-buy campaigns keep
// the single price (nil). A free gift costs 0 (the only 0 that is a fact).
func discountPrice(c obj) *model.Money {
	if len(c) == 0 || (truthy(c["MinQuantity"]) && number(c["MinQuantity"]).Cmp(one) > 0) {
		return nil
	}
	if c["Type"] == freeCampaign {
		z := model.Money{}
		return &z
	}
	if m := money(c["CampaignPrice"]); m != nil && !m.IsZero() {
		return m
	}
	return nil
}

// offerText is _offer(): text for a real campaign, nil for none.
func offerText(c obj) *string {
	if len(c) == 0 {
		return nil
	}
	qty, total := c["MinQuantity"], money(c["TotalPrice"])
	if truthy(qty) && number(qty).Cmp(one) > 0 && total != nil && !total.IsZero() {
		return strPtr(fmt.Sprintf("%s for %s kr", pyStr(qty), kr(*total)))
	}
	if pct := c["DiscountPercent"]; truthy(pct) {
		if dp := discountPrice(c); dp != nil && !dp.IsZero() {
			return strPtr(fmt.Sprintf("-%s %%", pyStr(pct)))
		}
	}
	if truthy(c["Type"]) {
		return strPtr("Tilbud") // a campaign we cannot describe
	}
	return nil
}

// until is _until(): the last Danish day of the campaign. IntervalEnd is UTC.
func until(c obj) *model.Date {
	end := timeVal(c["IntervalEnd"])
	if end == nil {
		return nil
	}
	var d model.Date
	if end.Aware() {
		y, m, day := end.Time().In(dk).Date()
		d, _ = model.NewDate(y, m, day)
	} else {
		d = end.Date()
	}
	return &d
}

// product is _product(): one search hit.
func product(hit obj) model.Product {
	avail := objOrEmpty(hit["Availability"])
	campaign := objOrEmpty(hit["Campaign"])
	price := requiredMoney(hit["Price"], "price") // never 0 kr for a missing price
	now := discountPrice(campaign)
	if now != nil && now.Cmp(price) >= 0 { // seen live: a "Dagstilbud" above the normal price
		now = nil
	}
	var offer *string
	if now != nil {
		offer = offerText(campaign)
	} else if dp := discountPrice(campaign); dp == nil || dp.IsZero() {
		offer = offerText(campaign)
	}
	if campaign["Type"] == freeCampaign && now != nil {
		offer = strPtr("Gratis")
	} else if offer != nil && *offer == "Tilbud" && now != nil {
		offer = strPtr(fmt.Sprintf("Tilbud, før %s kr", kr(price)))
	}
	var limit *int
	if v := campaign["MaxQuantity"]; offer != nil && truthy(v) {
		n := toInt(v)
		limit = &n
		offer = strPtr(fmt.Sprintf("%s (max %d stk.)", *offer, n))
	}
	p := model.NewProduct("nemlig", pyStr(field(hit, "Id")), textOf(withDefault(hit, "Name", "")), price)
	p.Brand = optStr(hit["Brand"])
	p.Size = optStr(hit["Description"])
	p.Unit = unitOf(hit["UnitPriceLabel"])
	p.OnOffer = offer != nil
	p.OfferText = offer
	p.OfferUntil = until(campaign)
	p.Available = truthy(withDefault(avail, "IsDeliveryAvailable", true)) &&
		truthy(withDefault(avail, "IsAvailableInStock", true))
	p.Category = optStr(hit["Category"])
	if now != nil {
		p.Price = *now
		p.UnitPrice = money(campaign["CampaignUnitPrice"])
		p.BeforePrice = &price
	} else {
		p.UnitPrice = money(hit["UnitPriceCalc"])
	}
	if offer != nil && strings.HasPrefix(*offer, "Gratis") {
		p.Campaign = strPtr("Gratis")
	} else {
		p.Campaign = rawStr(hit["CampaignAttribute"])
	}
	p.MaxPerOrder = limit
	return p
}

// dk is Danish time. Loaded from the embedded tzdata, so it works on a machine without zoneinfo.
var dk = mustLoad("Europe/Copenhagen")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
