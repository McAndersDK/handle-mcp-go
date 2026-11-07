package bilkatogo

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// units are Bilka's unit words as the other store writes them.
var units = map[string]string{"kg": "kg", "ltr": "l", "l": "l", "stk": "stk"}

// genericOffer: an offerDescription with no real text (docs/research.md).
const genericOffer = "Se Her"

// Search finds products in the store behind the cart's delivery address.
func (s *Store) Search(ctx context.Context, query string, limit int) (_ []model.Product, err error) {
	defer guard(&err)
	return s.algolia(ctx, query, limit, false, nil)
}

// Offers are the products on offer in our store: Skarp pris, leaflet offers and mix offers.
func (s *Store) Offers(ctx context.Context, limit int) (_ []model.Product, err error) {
	defer guard(&err)
	return s.algolia(ctx, "", limit, true, nil)
}

// algolia searches, lists offers, or (with ids) finds the products with those Bilka item numbers.
func (s *Store) algolia(ctx context.Context, query string, limit int, offers bool, ids []string) ([]model.Product, error) {
	if !store.HasLogin(ctx, name) { // the assortment, prices and offers follow the address
		return nil, store.Errorf("BilkaToGo viser priser og tilbud for butikken bag din leveringsadresse, " +
			"så et login skal til. Kør: handle-mcp login bilkatogo")
	}
	data, err := s.cartJSON(ctx)
	if err != nil {
		return nil, err
	}
	addr := readAddress(data)
	if addr.sid == "" {
		return nil, store.Errorf("Ingen leveringsadresse valgt i BilkaToGo. Vælg hjemlevering på bilkatogo.dk.")
	}
	sid := addr.sid
	filters := "isInAssortmentIn:" + sid + " AND nonsearchable:false"
	if offers { // isInOffer is a list of store ids, not true/false
		filters = "isInOffer:" + sid + " AND " + filters
	}
	if len(ids) > 0 { // a recipe's products may be hidden from search: no nonsearchable filter
		terms := make([]string, len(ids))
		for i, id := range ids {
			if !isItemNumber(id) { // never let anything but digits into a filter
				bad("item number %q is not digits", id)
			}
			terms[i] = "objectID:" + id
		}
		filters = "(" + strings.Join(terms, " OR ") + ") AND isInAssortmentIn:" + sid
	}
	attributes := []string{"name", "brand", "gtin", "netcontent", "productType", "deepestCategoryPath",
		"price", // the price without the offer
		"storeData." + sid}
	quoted := make([]string, len(attributes))
	for i, a := range attributes {
		quoted[i] = pyJSONString(a)
	}
	params := pairs{
		{"query", query},
		{"hitsPerPage", strconv.Itoa(limit)},
		// Algolia needs every word by default: "cheasy skyr passionsfrugt" gives 0 hits when that
		// flavour is not sold. Then retry with fewer words, like nemlig does.
		{"removeWordsIfNoResults", "allOptional"},
		{"filters", filters},
		{"attributesToRetrieve", "[" + strings.Join(quoted, ", ") + "]"},
	}
	body := obj{"requests": []any{obj{"indexName": algoliaIndex, "params": params.encode()}}}
	header := http.Header{}
	for _, kv := range algoliaHeaders {
		header[kv[0]] = []string{kv[1]}
	}
	resp, err := s.send(ctx, call{method: "POST", url: algoliaURL, header: header, json: body})
	if err != nil {
		return nil, err
	}
	what := "søgning"
	if offers {
		what = "tilbud"
	}
	if resp.Status != 200 {
		return nil, store.Errorf("BilkaToGo-%s fejlede (%d).", what, resp.Status)
	}
	answer, err := asDict(resp, what)
	if err != nil {
		return nil, err
	}
	hits, ok := hitsOf(answer)
	if !ok {
		return nil, store.Errorf("BilkaToGo-%s har ændret format.", what)
	}
	out := []model.Product{}
	for _, h := range hits {
		if p := product(asObj(h), sid, what); p != nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

// hitsOf is ["results"][0]["hits"]: false when a step is missing.
func hitsOf(answer obj) ([]any, bool) {
	results, ok := answer["results"].([]any)
	if !ok || len(results) == 0 {
		return nil, false
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return nil, false
	}
	hits, present := first["hits"]
	if !present {
		return nil, false
	}
	list, ok := hits.([]any)
	if !ok {
		bad("hits is not a list") // Python fails later, outside the format check
	}
	return list, true
}

// product is _product(): nil for a hit the store does not sell. A missing price is a format
// change, never a free product (ADR 0005); an explicit 0 is 0.00 kr.
func product(hit obj, sid, what string) *model.Product {
	data := objOrEmpty(objOrEmpty(hit["storeData"])[sid])
	if len(data) == 0 {
		return nil
	}
	unit := ""
	if u := data["unitsOfMeasurePriceUnit"]; truthy(u) {
		unit = pystr.Lower(strings.TrimRight(mustText(u), "."))
	}
	if data["price"] == nil {
		raise(store.Errorf("BilkaToGo-%s har ændret format.", what))
	}
	now, normal := kr(data["price"]), kr(hit["price"])
	var before *model.Money
	if normal.Cmp(now) > 0 {
		before = &normal
	}
	offer := optStr(data["offerDescription"])
	if offer != nil && *offer == genericOffer { // a leaflet offer with no text of its own
		text := "Tilbud"
		if before != nil {
			text = strings.ReplaceAll("Tilbud, før "+fmt2(*before)+" kr", ".", ",")
		}
		offer = &text
	}
	p := model.NewProduct(name, pyStr(field(hit, "objectID")), textOf(withDefault(hit, "name", "")), now)
	p.Brand = optStr(hit["brand"])
	p.Size = optStr(hit["netcontent"])
	if unit != "" {
		// with the offer; unitsOfMeasurePrice is without. Both missing: unknown, not 0.00 kr/unit.
		shown := data["unitsOfMeasureShowPrice"]
		if !truthy(shown) {
			shown = data["unitsOfMeasurePrice"]
		}
		if shown != nil {
			u := kr(shown)
			p.UnitPrice = &u
		}
	}
	if mapped, ok := units[unit]; ok {
		p.Unit = &mapped
	} else if unit != "" {
		p.Unit = &unit
	}
	p.EAN = optStr(hit["gtin"])
	p.OnOffer = offer != nil
	p.OfferText = offer
	p.Campaign = campaign(offer)
	if offer != nil {
		p.BeforePrice = before
		if n := toIntOrZero(data["offerMax"]); n != 0 {
			p.MaxPerOrder = &n
		}
	}
	p.Available = truthy(data["inStock"])
	var parts []string
	for _, x := range []any{hit["deepestCategoryPath"], hit["productType"]} {
		if truthy(x) {
			parts = append(parts, mustText(x))
		}
	}
	if len(parts) > 0 {
		p.Category = strPtr(strings.Join(parts, " > "))
	}
	return &p
}
