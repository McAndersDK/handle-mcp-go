package nemlig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// offerLists on /tilbud are matched on the heading. The other lists on the page are categories.
var offerLists = map[string]string{
	"skarp pris":      "Skarp pris",
	"ugens priskup":   "Priskup",
	"priskup":         "Priskup",
	"dagstilbud":      "Dagstilbud",
	"undgå madspild":  "Undgå madspild",
	"en gave til dig": "Gratis",
}

var offerUnits = map[string]string{"Weight": "kg", "Volume": "l", "Pieces": "stk"}

// isMulti is MULTI.match: "Mix 2 stk. 35,-" or "2 stk. 15,-". \d is any Unicode digit.
func isMulti(text string) bool {
	text = strings.TrimPrefix(text, "Mix ")
	rest := strings.TrimLeftFunc(text, pystr.IsDigit)
	return len(rest) < len(text) && strings.HasPrefix(rest, " stk.")
}

// offerProduct is offer_product(): one product from a list, or false if it is not cheaper than
// its normal price. Prices are in øre. No price is a format change, never "Gratis".
func offerProduct(p obj, campaign string) (model.Product, bool) {
	price := fromOre(field(p, "price"))
	var before *model.Money
	if v := p["priceOriginal"]; v != nil {
		b := fromOre(v)
		before = &b
	}
	if before != nil && price.Cmp(*before) >= 0 {
		return model.Product{}, false
	}
	var limit *int
	if v := p["labelMaxQuantity"]; truthy(v) {
		n := toInt(v)
		limit = &n
	}
	multi := "" // "Mix 2 stk. 35,-" or "2 stk. 15,-"
	for _, l := range listOrEmpty(p["campaignLines"]) {
		ln := asObj(l)
		text := ln["text"]
		if !truthy(text) {
			continue
		}
		s, ok := text.(string)
		if !ok {
			bad("campaign text")
		}
		if isMulti(s) {
			multi = s
			break
		}
	}
	hasBefore := before != nil && !before.IsZero()
	var text string
	switch {
	case campaign == "Gratis" || (price.IsZero() && hasBefore):
		campaign, text = "Gratis", "Gratis"
	case multi != "":
		text = multi
	case hasBefore:
		text = fmt.Sprintf("Tilbud, før %s kr", kr(*before))
	default:
		text = campaign
	}
	if limit != nil {
		text += fmt.Sprintf(" (max %d stk.)", *limit)
	}
	perUnit := objOrEmpty(p["pricePerUnit"])
	out := model.NewProduct(name, pyStr(field(p, "id")), textOf(withDefault(p, "title", "")), price)
	out.Size = optStr(p["description"])
	if v := perUnit["value"]; v != nil {
		u := fromOre(v)
		out.UnitPrice = &u
	}
	if t, ok := perUnit["unitType"].(string); ok {
		if unit, known := offerUnits[t]; known {
			out.Unit = strPtr(unit)
		}
	}
	out.OnOffer = true
	out.OfferText = &text
	out.BeforePrice = before
	out.Available = withDefault(objOrEmpty(p["availability"]), "type", "Available") == "Available"
	out.Campaign = strPtr(campaign)
	out.MaxPerOrder = limit
	return out, true
}

type offerList struct {
	name  string
	items any
}

// Offers are every product on nemlig's offer lists (/tilbud): Skarp pris, Priskup, Dagstilbud,
// Undgå madspild and gifts. limit is per list. Read only. Works without a login: the offers are
// public; with one, they fit the household's delivery time.
func (s *Store) Offers(ctx context.Context, limit int) (_ []model.Product, err error) {
	defer guardAs(&err, "nemligs tilbudslister har ændret format.")
	p := params{}.add("path", "/tilbud")
	slot, err := s.timeslotID(ctx)
	if err != nil {
		return nil, err
	}
	if slot != "" {
		p = p.add("timeslotId", slot) // sic: the page call spells it so, the dialog not
	}
	data, err := s.public(ctx, call{method: "GET", url: bff + "/page", params: p})
	if err != nil {
		return nil, err
	}
	var lists []offerList
	for _, b := range listOrEmpty(objOrEmpty(data)["pageContent"]) {
		block := asObj(b)
		title := objOrEmpty(block["header"])["title"]
		if !truthy(title) {
			title = ""
		}
		if list, ok := offerLists[pystr.Lower(pystr.Strip(pyStr(title)))]; ok {
			lists = append(lists, offerList{list, block["products"]})
		}
	}
	if len(lists) == 0 {
		return nil, store.Errorf("Fandt ingen tilbudslister hos nemlig. Siden kan have ændret sig.")
	}
	var products []model.Product
	seen := map[string]bool{}
	add := func(p model.Product) {
		if !seen[p.ID] {
			seen[p.ID] = true
			products = append(products, p)
		}
	}
	broken := 0
	for _, l := range lists {
		found, ok := readList(l, limit)
		if !ok { // one broken list must not hide the others
			broken++
			continue
		}
		for _, p := range found {
			add(p)
		}
	}
	if broken == len(lists) {
		return nil, store.Errorf("nemligs tilbudslister har ændret format.")
	}
	// "En gave til dig" is often empty (live 2026-11-02), but a search for "gratis" finds the free
	// gifts (ProductCampaignFreeProduct). A free-text search, so it may change. Search needs a
	// login; without one the lists above are the whole answer.
	if s.hasLogin(ctx) {
		gifts, err := s.Search(ctx, "gratis", 50)
		var se *store.StoreError
		switch {
		case errors.As(err, &se):
			slog.Warn("nemlig-gaver fejlede", "error", se.Msg)
		case err != nil:
			return nil, err
		}
		for _, g := range gifts {
			if g.Campaign != nil && *g.Campaign == "Gratis" {
				add(g)
			}
		}
	}
	if products == nil {
		products = []model.Product{}
	}
	return products, nil
}

// readList reads one list. false when its shape is not what we know. Every product is read
// before the limit cuts, so a broken product shows even past the limit.
func readList(l offerList, limit int) (found []model.Product, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, isShape := r.(shapeError); !isShape {
				panic(r)
			}
			slog.Warn("nemlig-tilbudslisten har ændret format", "list", l.name)
			found, ok = nil, false
		}
	}()
	for _, it := range listOrEmpty(l.items) {
		if p, real := offerProduct(asObj(it), l.name); real {
			found = append(found, p)
		}
	}
	return sliceTo(found, limit), true
}
