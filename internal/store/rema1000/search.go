package rema1000

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

const maxProductPages = 10

// apiPrice is one price period. A product has the current one first and often the next one after.
type apiPrice struct {
	Price            json.Number `json:"price"`
	PriceOverMax     json.Number `json:"price_over_max_quantity"`
	MaxQuantity      json.Number `json:"max_quantity"`
	IsAdvertised     bool        `json:"is_advertised"`
	IsCampaign       bool        `json:"is_campaign"`
	StartingAt       string      `json:"starting_at"`
	EndingAt         string      `json:"ending_at"`
	CompareUnit      string      `json:"compare_unit"`
	CompareUnitPrice json.Number `json:"compare_unit_price"`
}

// apiProduct is a product from search, offers and recipes. Only what an answer needs.
type apiProduct struct {
	ID        json.Number `json:"id"`
	Name      string      `json:"name"`
	Underline string      `json:"underline"` // "0.5 LTR. / ARLA": size, then brand
	Barcodes  []string    `json:"barcodes"`
	Category  *struct {
		Name string `json:"name"`
	} `json:"category"`
	Prices []apiPrice `json:"prices"`
}

// Search finds products. The API is public and the same for everyone, but like every method it
// needs a chosen pickup store first (gate.go).
func (s *Store) Search(ctx context.Context, query string, limit int) ([]model.Product, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.products(ctx, url.Values{"query": {query}}, limit, "søgningen")
}

// Offers lists the products in REMA's weekly leaflet (the label Avisvare), the same list the
// webshop shows on /avisvarer.
func (s *Store) Offers(ctx context.Context, limit int) ([]model.Product, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	q := url.Values{"filter[labels]": {advertisedLabel}, "sort": {"department"}}
	return s.products(ctx, q, limit, "tilbudslisten")
}

func (s *Store) products(ctx context.Context, q url.Values, limit int, what string) ([]model.Product, error) {
	limit = max(limit, 1)
	rows, err := pages[apiProduct](ctx, s, call{method: "GET", path: "search/products", query: q},
		what, min(limit, 100), maxProductPages, func(r []apiProduct) bool { return len(r) >= limit })
	if err != nil {
		return nil, err
	}
	today := s.today()
	out := []model.Product{}
	for _, row := range rows {
		p, ok, err := toProduct(row, today)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// money reads a price. An empty number is "not there"; text is an error.
func money(n json.Number) (m model.Money, ok bool, err error) {
	if n == "" {
		return model.Money{}, false, nil
	}
	m, err = model.ParseMoney(string(n))
	if err != nil {
		return model.Money{}, false, store.Errorf("REMA 1000 sendte en pris, der ikke er et tal.")
	}
	return m, true, nil
}

// dayOf is the date part of "2026-11-07T00:00:00+00:00".
func dayOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ""
}

// activePrice is the period that holds today. REMA lists the current one first, so that is the
// fallback when no period names today. A campaign's last day counts as a day of the campaign.
func activePrice(prices []apiPrice, today string) (apiPrice, bool) {
	for _, p := range prices {
		if from, to := dayOf(p.StartingAt), dayOf(p.EndingAt); from != "" && to != "" && from <= today && today <= to {
			return p, true
		}
	}
	if len(prices) > 0 {
		return prices[0], true
	}
	return apiPrice{}, false
}

// units maps REMA's compare unit to the model's three.
var units = map[string]string{"kg": "kg", "ltr": "l", "l": "l", "stk": "stk"}

// toProduct maps a product. ok is false when it has no price: it cannot be bought.
func toProduct(row apiProduct, today string) (model.Product, bool, error) {
	active, ok := activePrice(row.Prices, today)
	if !ok || row.ID == "" {
		return model.Product{}, false, nil
	}
	price, has, err := money(active.Price)
	if err != nil || !has {
		return model.Product{}, false, err
	}
	p := model.NewProduct(name, string(row.ID), strings.TrimSpace(row.Name), price)
	size, brand, _ := strings.Cut(row.Underline, " / ")
	p.Size, p.Brand = text(size), text(brand)
	if len(row.Barcodes) > 0 {
		p.EAN = text(row.Barcodes[0])
	}
	if row.Category != nil {
		p.Category = text(row.Category.Name)
	}
	if u, ok := units[strings.ToLower(active.CompareUnit)]; ok {
		if per, has, err := money(active.CompareUnitPrice); err != nil {
			return model.Product{}, false, err
		} else if has {
			p.UnitPrice, p.Unit = &per, &u
		}
	}
	if active.IsAdvertised {
		c := "Avisvare"
		p.Campaign = &c
	}
	if !active.IsCampaign {
		return p, true, nil
	}
	return withOffer(p, active, row.Prices)
}

// withOffer fills in the offer facts of a campaign price. The price without the offer is the
// price over the limit when REMA gives one, else the next period that is not a campaign.
func withOffer(p model.Product, active apiPrice, all []apiPrice) (model.Product, bool, error) {
	p.OnOffer = true
	if to := dayOf(active.EndingAt); to != "" && !strings.HasPrefix(to, "2099") {
		if d, err := model.ParseDate(to); err == nil {
			p.OfferUntil = &d
		}
	}
	offer := "Tilbud"
	if limit, err := strconv.Atoi(string(active.MaxQuantity)); err == nil && limit > 0 {
		p.MaxPerOrder = &limit
		offer = fmt.Sprintf("Tilbud, maks. %d stk.", limit)
	}
	p.OfferText = &offer
	before := active.PriceOverMax
	if over, has, _ := money(before); !has || over.Sign() <= 0 {
		before = ""
		for _, o := range all {
			if !o.IsCampaign && o.Price != "" {
				before = o.Price
				break
			}
		}
	}
	if m, has, err := money(before); err != nil {
		return model.Product{}, false, err
	} else if has {
		p.BeforePrice = &m
	}
	return p, true, nil
}

// text is a trimmed string, or nil when empty.
func text(s string) *string {
	if s = strings.TrimSpace(s); s != "" {
		return &s
	}
	return nil
}
