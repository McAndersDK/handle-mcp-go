package rema1000

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantStoreError(t *testing.T, err error, contains string) {
	t.Helper()
	var se *store.StoreError
	if !errors.As(err, &se) || !strings.Contains(se.Msg, contains) {
		t.Fatalf("got %v, want a StoreError with %q", err, contains)
	}
}

// --- the gate ---------------------------------------------------------------

func TestGateNoStoreChosen(t *testing.T) {
	isolate(t)
	f := newFake(t)
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue})
	s := newStore(f)
	for name, call := range map[string]func() error{
		"search": func() error { _, err := s.Search(context.Background(), "mælk", 3); return err },
		"cart":   func() error { _, err := s.GetCart(context.Background()); return err },
		"slots":  func() error { _, err := s.DeliverySlots(context.Background(), 3); return err },
		"recipe": func() error { _, err := s.Recipes(context.Background(), "kylling", 3); return err },
		"set":    func() error { _, err := s.SetQuantities(context.Background(), nil); return err },
	} {
		err := call()
		if err == nil || err.Error() != msgNoStore {
			t.Errorf("%s: %v", name, err)
		}
	}
	if f.count("GET", "/search/") != 0 || f.count("POST", "") != 0 {
		t.Error("work was done before the gate")
	}
}

// --- search, offers ---------------------------------------------------------

func TestSearchMapsProducts(t *testing.T) {
	_, s := harness(t)
	got, err := s.Search(context.Background(), "mælk", 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	p := got[0]
	if p.ID != "20302" || p.Name != "SØDMÆLK 3,5%" || p.Price.String() != "7.95" {
		t.Errorf("product: %s", asJSON(t, p))
	}
	if p.UnitPrice == nil || p.UnitPrice.String() != "15.90" || *p.Unit != "l" {
		t.Errorf("unit price: %s", asJSON(t, p))
	}
	if *p.Size != "0.5 LTR." || *p.Brand != "ARLA" || *p.EAN != "5700426101026" || p.OnOffer || !p.Available {
		t.Errorf("fields: %s", asJSON(t, p))
	}
}

func TestOffersMapCampaigns(t *testing.T) {
	f, s := harness(t)
	got, err := s.Offers(context.Background(), 10)
	if err != nil || len(got) != 6 {
		t.Fatalf("%d offers, err %v", len(got), err)
	}
	p := got[0]
	if !p.OnOffer || p.Price.String() != "3.00" || p.OfferUntil.String() != "2026-11-07" || p.BeforePrice.String() != "4.00" {
		t.Errorf("offer: %s", asJSON(t, p))
	}
	if p.Campaign == nil || *p.Campaign != "Avisvare" || p.OfferText == nil {
		t.Errorf("campaign: %s", asJSON(t, p))
	}
	found := false
	for _, r := range f.requests() {
		found = found || strings.Contains(r.Query, "filter%5Blabels%5D=9001")
	}
	if !found {
		t.Error("the leaflet label was not asked for")
	}
}

func TestActivePriceFollowsTheDay(t *testing.T) {
	prices := []apiPrice{
		{Price: "3", IsCampaign: true, StartingAt: "2026-11-05T00:00:00+00:00", EndingAt: "2026-11-07T00:00:00+00:00"},
		{Price: "4", StartingAt: "2026-11-08T00:00:00+00:00", EndingAt: "2099-12-31T00:00:00+00:00"},
	}
	if p, _ := activePrice(prices, "2026-11-07"); p.Price != "3" {
		t.Error("the campaign's last day is a campaign day")
	}
	if p, _ := activePrice(prices, "2026-11-08"); p.Price != "4" {
		t.Error("the day after is the normal price")
	}
	if p, _ := activePrice(prices, "2030-01-01"); p.Price != "4" {
		t.Error("a day inside the long period")
	}
}

// --- the cart ----------------------------------------------------------------

func TestCartWithoutTodaysListIsEmpty(t *testing.T) {
	f, s := harness(t)
	f.addList("Min liste", true, fakeItem{ID: 1, StoreItemID: 5, Amount: 9}) // the human's own list
	cart, err := s.GetCart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cart.Lines) != 0 || len(cart.Warnings) != 1 || !strings.Contains(cart.Warnings[0], "handle-mcp 2026-11-07") {
		t.Errorf("cart: %s", asJSON(t, cart))
	}
	if *cart.DeliveryMode != "pickup" || *cart.PickupStore != "REMA 1000 Eksempelby" {
		t.Errorf("mode: %s", asJSON(t, cart))
	}
	if f.count("GET", "/shoppinglists/") != 0 {
		t.Error("another list was read")
	}
}

func TestCartReadsTodaysListAndLeavesBoughtOut(t *testing.T) {
	f, s := harness(t)
	f.addList("handle-mcp 2026-11-06", false, fakeItem{ID: 1, StoreItemID: 5, Amount: 9}) // yesterday
	f.addList("handle-mcp 2026-11-07", false,
		fakeItem{ID: 10, StoreItemID: 401042, Amount: 2}, fakeItem{ID: 11, StoreItemID: 7, Amount: 1, Bought: true})
	cart, err := s.GetCart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cart.Lines) != 1 || cart.Lines[0].ProductID != "401042" || cart.Lines[0].Quantity != 2 {
		t.Fatalf("lines: %s", asJSON(t, cart.Lines))
	}
	// the fixture's offer price is 29 with no limit: 2 * 29
	if cart.Lines[0].LineTotal.String() != "58.00" || cart.Total.String() != "58.00" || cart.ProductsTotal.String() != "58.00" {
		t.Errorf("totals: %s", asJSON(t, cart))
	}
	for _, r := range f.requests() {
		if strings.Contains(r.Query, "users") {
			t.Errorf("the list was read with users: %s", r.Query)
		}
	}
}

func TestLineTotalAppliesTheLimit(t *testing.T) {
	var it apiItem
	raw := `{"amount": 5, "store_item": {"pricing": {"price": 10, "max_quantity": 2, "price_over_max": 15, "deposit": 1.5, "is_on_discount": true}}}`
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatal(err)
	}
	total, deposit, err := lineTotal(it)
	if err != nil || total.String() != "65.00" || deposit.String() != "7.50" { // 2*10 + 3*15
		t.Errorf("total %v deposit %v err %v", total, deposit, err)
	}
}

// --- set quantities ------------------------------------------------------------

func TestSetQuantitiesCreatesTodaysListAndVerifies(t *testing.T) {
	f, s := harness(t)
	own := f.addList("Min liste", true, fakeItem{ID: 1, StoreItemID: 5, Amount: 9})
	res, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "20302", Quantity: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].OK || *res[0].Applied != 2 || res[0].Message != nil {
		t.Fatalf("result: %s", asJSON(t, res))
	}
	if len(f.lists) != 2 || f.lists[1].Name != "handle-mcp 2026-11-07" || f.lists[1].Primary {
		t.Fatalf("lists: %v %v", f.lists[0], f.lists[1])
	}
	if !own.Primary || len(own.Items) != 1 || own.Items[0].Amount != 9 {
		t.Errorf("the human's own list changed: %+v", own)
	}
	var created string
	for _, r := range f.requests() {
		if r.Method == "POST" && r.Path == "/api/v2/shoppinglists" {
			created = r.Body
		}
	}
	if created != `{"name":"handle-mcp 2026-11-07","primary":false,"items":[]}` {
		t.Errorf("create body: %s", created)
	}
}

func TestSetQuantitiesAddChangeRemove(t *testing.T) {
	f, s := harness(t)
	l := f.addList("handle-mcp 2026-11-07", false,
		fakeItem{ID: 10, StoreItemID: 111, Amount: 1}, fakeItem{ID: 11, StoreItemID: 222, Amount: 4},
		fakeItem{ID: 12, StoreItemID: 333, Amount: 1, Bought: true}, fakeItem{ID: 13, StoreItemID: 444, Amount: 3})
	res, err := s.SetQuantities(context.Background(), []store.CartItem{
		{ProductID: "111", Quantity: 1}, // unchanged: no request
		{ProductID: "222", Quantity: 6}, // change
		{ProductID: "333", Quantity: 2}, // bought in the app: brought back
		{ProductID: "444", Quantity: 0}, // remove
		{ProductID: "555", Quantity: 3}, // add
		{ProductID: "666", Quantity: 0}, // not there: nothing to do
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("not ok: %s", asJSON(t, r))
		}
	}
	got := map[int]fakeItem{}
	for _, it := range l.Items {
		got[it.StoreItemID] = it
	}
	if got[111].Amount != 1 || got[222].Amount != 6 || got[333].Amount != 2 || got[333].Bought || got[555].Amount != 3 {
		t.Errorf("items: %+v", l.Items)
	}
	if _, there := got[444]; there {
		t.Error("444 was not removed")
	}
	reqs := map[string]string{}
	for _, r := range f.requests() {
		if r.Method != "GET" {
			reqs[r.Method+" "+r.Path] = r.Body
		}
	}
	base := "/api/v2/shoppinglists/" + strconv.Itoa(l.ID)
	if reqs["POST "+base+"/items"] != `{"amount":3,"store_id":1,"store_item_id":555}` ||
		reqs["PATCH "+base+"/items/11"] != `{"amount":6}` ||
		reqs["PATCH "+base+"/items/12"] != `{"amount":2,"bought":false}` ||
		reqs["PATCH "+base] != `{"items":[{"id":13,"delete":true}]}` {
		t.Errorf("requests: %v", reqs)
	}
	if len(reqs) != 4 {
		t.Errorf("%d writes, want 4: %v", len(reqs), reqs)
	}
}

func TestSetQuantitiesWarnsWhenTheNewListIsPrimary(t *testing.T) {
	f, s := harness(t)
	f.createPrimary = true
	res, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "20302", Quantity: 1}})
	if err != nil || !res[0].OK || res[0].Message == nil || *res[0].Message != primaryWarning {
		t.Fatalf("result %s, err %v", asJSON(t, res), err)
	}
	for _, r := range f.requests() {
		if r.Method == "PATCH" && !strings.Contains(r.Path, "/items/") {
			t.Errorf("the list was patched: %s %s", r.Path, r.Body)
		}
	}
}

func TestSetQuantitiesReportsARefusedProductAndGoesOn(t *testing.T) {
	f, s := harness(t)
	f.refusePOST = "999"
	res, err := s.SetQuantities(context.Background(), []store.CartItem{
		{ProductID: "999", Quantity: 1}, {ProductID: "20302", Quantity: 1}, {ProductID: "abc", Quantity: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].OK || res[0].Message == nil || !res[1].OK || res[2].OK || res[2].Message == nil {
		t.Errorf("results: %s", asJSON(t, res))
	}
}

func TestRemovingFromAMissingListCreatesNothing(t *testing.T) {
	f, s := harness(t)
	res, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "20302", Quantity: 0}})
	if err != nil || !res[0].OK || len(f.lists) != 0 || f.count("POST", "") != 0 {
		t.Errorf("result %s, lists %d, err %v", asJSON(t, res), len(f.lists), err)
	}
}

// --- delivery -------------------------------------------------------------------

func TestSlotsAndInfo(t *testing.T) {
	_, s := harness(t)
	slots, err := s.DeliverySlots(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	// the fixture has 2 slots on the 7th (both unavailable) and 3 on each following day
	if len(slots) != 5 {
		t.Fatalf("%d slots: %s", len(slots), asJSON(t, slots))
	}
	first := slots[0]
	if first.Start.String() != "2026-11-07T14:00:00+01:00" || first.Available || *first.Kind != "Afhentning" ||
		*first.PickupStore != "REMA 1000 Eksempelby" || first.Price != nil || first.Note == nil {
		t.Errorf("slot: %s", asJSON(t, first))
	}
	if !slots[2].Available {
		t.Errorf("slot: %s", asJSON(t, slots[2]))
	}
	info, err := s.DeliveryInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *info.DeliveryMode != "pickup" || info.ChosenStart != nil || info.NextAvailable.String() != "2026-11-08" ||
		!strings.Contains(*info.Note, "vælges og betales hos REMA 1000") {
		t.Errorf("info: %s", asJSON(t, info))
	}
}

func TestTermsAndOrders(t *testing.T) {
	_, s := harness(t)
	terms, err := s.Terms(context.Background())
	if err != nil || terms.HomeDelivery || *terms.DeliveryMode != "pickup" || terms.DeliveryFee != nil || terms.MinimumOrder != nil {
		t.Errorf("terms: %s, err %v", asJSON(t, terms), err)
	}
	if _, err := s.Orders(context.Background(), 5); !errors.Is(err, store.ErrNotImplemented) {
		t.Errorf("orders: %v", err)
	}
	if _, err := s.Order(context.Background(), "1"); !errors.Is(err, store.ErrNotImplemented) {
		t.Errorf("order: %v", err)
	}
}

// --- recipes ----------------------------------------------------------------------

func TestRecipeLists(t *testing.T) {
	_, s := harness(t)
	got, err := s.Recipes(context.Background(), "kylling", 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	r := got[0]
	if r.ID != "kylling-i-tarteletter" || r.URL != "https://shop.rema1000.dk/opskrifter/kylling-i-tarteletter" ||
		*r.Persons != 4 || r.Time != nil || *r.Group != "Aftensmad" {
		t.Errorf("recipe: %s", asJSON(t, r))
	}
	feat, err := s.FeaturedRecipes(context.Background(), 1)
	if err != nil || len(feat) != 1 {
		t.Errorf("featured: %v %v", feat, err)
	}
}

func TestRecipesByTag(t *testing.T) {
	f, s := harness(t)
	cat := "aftensmad"
	got, err := s.RecipesByTag(context.Background(), []string{"Avisopskrifter"}, nil, 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	// the second tag is checked on the recipes: the fixture's recipes are not both "aftensmad"
	got, err = s.RecipesByTag(context.Background(), []string{"avisopskrifter"}, &cat, 5)
	if err != nil || len(got) != 0 {
		t.Errorf("both tags: %v %v", got, err)
	}
	_, err = s.RecipesByTag(context.Background(), []string{"findes ikke"}, nil, 5)
	wantStoreError(t, err, "Kendte tags")
	found := false
	for _, r := range f.requests() {
		found = found || strings.Contains(r.Query, "filter%5Btags.sys.id%5D=5Q5t9rpmNk4jp1ygKZDEHx")
	}
	if !found {
		t.Error("the tag was not filtered on")
	}
}

func TestRecipeDetailsScaleAndPickProducts(t *testing.T) {
	_, s := harness(t)
	eight := 8
	d, err := s.Recipe(context.Background(), "https://shop.rema1000.dk/opskrifter/kylling-i-tarteletter/", &eight)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "kylling-i-tarteletter" || *d.Persons != 8 || *d.IngredientsPersons != 4 || len(d.Method) != 2 {
		t.Errorf("details: %s", asJSON(t, d)[:400])
	}
	if len(d.Ingredients) != 20 || d.Ingredients[0] != "1 stk Hel kylling" || d.Ingredients[14] != "1 l kyllingefond" {
		t.Errorf("ingredients: %v", d.Ingredients)
	}
	byName := map[string]model.RecipeItem{}
	for _, it := range d.Items {
		byName[it.Ingredient] = it
	}
	if it := byName["Hel kylling"]; it.Product.ID != "404969" || it.Product.Quantity != 2 || it.HaveAtHome {
		t.Errorf("chicken: %s", asJSON(t, it))
	}
	if it := byName["hvide asparges"]; it.Product.Quantity != 4 { // 2 packs for 4 persons
		t.Errorf("asparagus: %s", asJSON(t, it))
	}
	if it := byName["salt"]; !it.HaveAtHome {
		t.Errorf("salt is a basic ingredient: %s", asJSON(t, it))
	}
	if len(d.Missing) != 1 || d.Missing[0] != "grønne asparges" { // no product; aspargeslage is excluded
		t.Errorf("missing: %v", d.Missing)
	}
	var sum model.Money
	for _, it := range d.Items {
		if !it.HaveAtHome {
			sum = sum.Add(it.Product.Price.Mul(int64(it.Product.Quantity)))
		}
	}
	if sum.Cmp(d.Total) != 0 || d.Total.Sign() <= 0 {
		t.Errorf("total %s, items %s", d.Total, sum)
	}
	if _, err := s.Recipe(context.Background(), "findes-ikke", nil); err == nil {
		t.Error("an unknown recipe must fail")
	}
}
