package bilkatogo

// BilkaToGo recipes: real pages from bilkatogo.dk/inspiration/opskrifter (fixtures, 2026-11-06).
// The Go home of each test in upstream/tests/test_bilkatogo_recipes.py:
//
//	test_payload_is_read_as_json                    TestPayloadIsReadAsJSON (payload_test.go)
//	test_recipe_path_from_link_or_path              TestRecipePathFromLinkOrPath
//	test_recipe_path_must_be_a_path                 TestRecipePathMustBeAPath
//	test_category_path_from_danish_words            TestCategoryPathFromDanishWords
//	test_recipe_has_text_and_products               TestRecipeHasTextAndProducts
//	test_no_products_found_is_a_clear_error         TestNoProductsFoundIsAClearError
//	test_only_item_numbers_go_in_the_filter         TestOnlyItemNumbersGoInTheFilter
//	test_method_and_tips_in_one_paragraph           TestMethodAndTipsInOneParagraph
//	test_a_category_page_is_not_a_recipe            TestACategoryPageIsNotARecipe
//	test_featured_is_this_weeks_madplan             TestFeaturedIsThisWeeksMadplan
//	test_category_with_recipes                      TestCategoryWithRecipes
//	test_category_with_sub_categories_mixes_them    TestCategoryWithSubCategoriesMixesThem
//	test_unknown_category_lists_the_real_ones       TestUnknownCategoryListsTheRealOnes
//	test_tags_and_search_say_what_to_use            TestTagsAndSearchSayWhatToUse (and goldens recipes, recipes_by_tag)
//	test_new_build_id_is_read_again                 TestNewBuildIDIsReadAgain
//	test_no_build_id_is_a_clear_error               TestNoBuildIDIsAClearError
//	test_nemlig_recipes.py: test_bilkatogo_has_no_recipes_by_tag was removed upstream; the golden
//	cases featured_recipes, recipe, recipes and recipes_by_tag cover the failure paths.
//
// Tests that only Go has (the checks the port adds) follow the ones above.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// recipeWorld is a store that talks to the fake website and to the fake Bilka, with a login.
func recipeWorld(t *testing.T) (*harness, *siteFake) {
	t.Helper()
	h := envLogin(t)
	w := newSiteFake(h.f)
	h.s = New(&http.Client{Transport: w}, WithClock(func() time.Time { return h.clock }),
		WithSleep(func(context.Context, time.Duration) error { return nil }))
	return h, w
}

// karbonaderHits are three Algolia hits with the item numbers of the first products of karbonader.
func karbonaderHits(t *testing.T) []any {
	t.Helper()
	hits := fixture(t, "algolia_products").(obj)["hits"].([]any)[:3]
	for i, sku := range []string{"81498", "125887", "137133"} { // changed by hand
		hits[i].(obj)["objectID"] = sku
	}
	return hits
}

func scriptHits(h *harness, hits []any) {
	h.f.scripted[pAlgolia] = []any{obj{"results": []any{obj{"hits": hits}}}}
}

// lastAlgolia is the params of the last Algolia request, as the form it was sent in.
func lastAlgolia(t *testing.T, h *harness) (raw string, params url.Values) {
	t.Helper()
	reqs := h.f.all(pAlgolia)
	if len(reqs) == 0 {
		t.Fatal("no Algolia request")
	}
	raw = reqs[len(reqs)-1].Body.(obj)["requests"].([]any)[0].(obj)["params"].(string)
	params, _ = url.ParseQuery(raw)
	return raw, params
}

func groupsOf(rs []model.Recipe) map[string]bool {
	out := map[string]bool{}
	for _, r := range rs {
		if r.Group == nil {
			out["<nil>"] = true
		} else {
			out[*r.Group] = true
		}
	}
	return out
}

func TestRecipePathFromLinkOrPath(t *testing.T) {
	for _, given := range []string{
		karbonader, karbonader + "?x=1", karbonader + "#top", "/inspiration/opskrifter/aftensmad/danske-retter/karbonader",
		"aftensmad/danske-retter/karbonader", "  AFTENSMAD/Danske-Retter/Karbonader/ ",
		"http://bilkatogo.dk/inspiration/opskrifter/aftensmad/danske-retter/karbonader",
		"www.bilkatogo.dk/inspiration/opskrifter/aftensmad/danske-retter/karbonader",
		"//www.bilkatogo.dk/inspiration/opskrifter/aftensmad/danske-retter/karbonader",
	} {
		if got, err := recipePath(given); err != nil || got != "aftensmad/danske-retter/karbonader" {
			t.Errorf("recipePath(%q) = %q, %v", given, got, err)
		}
	}
}

func TestRecipePathMustBeAPath(t *testing.T) {
	for _, bad := range []string{
		"../etc", "karbo nader", "", "https://www.bilkatogo.dk/", "a//b", "a/../b", "aftensmad/%2e%2e", "aftensmad\nx",
		// another host, whatever the path says
		"https://www.nemlig.com/maaltider/opskrift/kebabfad-98001303",
		"https://evil.example/bilkatogo.dk/aftensmad/pizza",
		"https://bilkatogo.dk.evil.example/inspiration/opskrifter/aftensmad",
		"https://www.bilkatogo.dk@evil.example/inspiration/opskrifter/aftensmad",
		"https://www.bilkatogo.dk:8443/inspiration/opskrifter/aftensmad",
		"ftp://www.bilkatogo.dk/inspiration/opskrifter/aftensmad",
		"//evil.example/inspiration/opskrifter/aftensmad",
		"evil.example/inspiration/opskrifter/aftensmad",
	} {
		_, err := recipePath(bad)
		if err == nil || !strings.Contains(err.Error(), "Kender ikke") {
			t.Errorf("recipePath(%q): %v", bad, err)
		}
	}
}

func TestCategoryPathFromDanishWords(t *testing.T) {
	for given, want := range map[string]string{
		"Børn i køkkenet": "boern-i-koekkenet", "Aftensmad": "aftensmad", "aftensmad/danske-retter": "aftensmad/danske-retter",
		"Grønt og Å": "groent-og-aa",
	} {
		if got, err := categoryPath(given); err != nil || got != want {
			t.Errorf("categoryPath(%q) = %q, %v; want %q", given, got, err, want)
		}
	}
}

func TestRecipeHasTextAndProducts(t *testing.T) {
	h, w := recipeWorld(t)
	scriptHits(h, karbonaderHits(t))
	persons := 6
	r, err := h.s.Recipe(bg, karbonader, &persons)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "aftensmad/danske-retter/karbonader" || r.Name != "Karbonader" || r.URL != karbonader || r.Store != "bilkatogo" {
		t.Errorf("id, name, url: %q %q %q", r.ID, r.Name, r.URL)
	}
	if r.Persons == nil || *r.Persons != 4 || r.IngredientsPersons == nil || *r.IngredientsPersons != 4 || show(r.Time) != "35 min" {
		t.Errorf("persons %s / %s, time %s: fixed at 4, not 6", show(r.Persons), show(r.IngredientsPersons), show(r.Time))
	}
	if !strings.HasPrefix(show(r.Description), "Lav nemt klassiske karbonader") {
		t.Errorf("description %s", show(r.Description))
	}
	if img := show(r.Image); !strings.HasPrefix(img, "https://digitalassets.sallinggroup.com/") || !strings.Contains(img, "jcr:26b30edc") {
		t.Errorf("image %s", img)
	}
	if !slices.Equal([]string(r.Ingredients[:3]), []string{"500 g hakket svin", "1 æg", "1 løg"}) {
		t.Errorf("ingredients %v", r.Ingredients[:3])
	}
	for _, line := range r.Ingredients {
		if line == "Karbonader" || strings.Contains(line, "Forberedelse") {
			t.Errorf("a heading is in the ingredients: %q", line)
		}
	}
	if len(r.Method) != 1 || len(r.Method[0].Steps) != 11 || r.Method[0].Title != nil {
		t.Errorf("method %+v", r.Method)
	}
	if !strings.HasPrefix(show(r.Tip), "Du kan prøve at lave vegetariske karbonader") {
		t.Errorf("tip %s", show(r.Tip))
	}
	var ids []string
	sum := model.Money{}
	for _, it := range r.Items {
		ids = append(ids, it.Product.ID)
		sum = sum.Add(it.Product.Price)
		if it.Product.Quantity != 1 || it.HaveAtHome || len(it.Alternatives) != 0 || it.Ingredient != it.Product.Name {
			t.Errorf("item %+v: one pack, not at home, no alternatives", it)
		}
	}
	if !slices.Equal(ids, []string{"81498", "125887", "137133"}) { // the page's order
		t.Errorf("items %v", ids)
	}
	if len(r.Missing) != 12 || !slices.Contains(r.Missing, "17010") { // 15 item numbers, 3 sold here
		t.Errorf("missing %v", r.Missing)
	}
	if r.Total.Sign() <= 0 || r.Total.Cmp(sum) != 0 {
		t.Errorf("total %s, items add up to %s", r.Total, sum)
	}
	raw, _ := lastAlgolia(t, h)
	if !strings.Contains(raw, "objectID%3A81498+OR+objectID%3A125887") || !strings.Contains(raw, "17010") {
		t.Errorf("the filter did not name the item numbers: %s", raw)
	}
	if n := w.count("/inspiration/opskrifter/"); n != 1 {
		t.Errorf("%d reads of the build id, want 1", n)
	}
}

func TestNoProductsFoundIsAClearError(t *testing.T) {
	h, _ := recipeWorld(t)
	scriptHits(h, []any{})
	_, err := h.s.Recipe(bg, karbonader, nil)
	if msg := storeErr(t, err); !strings.Contains(msg, "ingen af opskriftens varer") {
		t.Errorf("message %q", msg)
	}
}

func TestOnlyItemNumbersGoInTheFilter(t *testing.T) {
	h, w := recipeWorld(t)
	text := recipeText("karbonader.payload.js")
	w.override["aftensmad/danske-retter/karbonader"] = strings.Replace(text, "curatedSkuList:[",
		`curatedSkuList:["1 OR isInOffer:1","12 34","","٣٤",null,7,true,void 0,["99"],{a:"1"},"0a","99999999999999999999",`, 1)
	scriptHits(h, []any{})
	_, err := h.s.Recipe(bg, karbonader, nil)
	storeErr(t, err) // nothing was found: an error, and the filter is what matters
	_, params := lastAlgolia(t, h)
	filters := params.Get("filters")
	ids := regexp.MustCompile(`objectID:(\w+)`).FindAllStringSubmatch(filters, -1)
	if len(ids) != 16 { // 15 from the page, and "99999999999999999999": digits
		t.Errorf("%d item numbers in %q, want 16", len(ids), filters)
	}
	for _, m := range ids {
		if !isItemNumber(m[1]) {
			t.Errorf("%q is in the filter", m[1])
		}
	}
	if strings.Contains(filters, "isInOffer") || strings.Contains(filters, " 34") || !strings.HasSuffix(filters, "AND isInAssortmentIn:9002") {
		t.Errorf("filter %q", filters)
	}
}

func TestMethodAndTipsInOneParagraph(t *testing.T) {
	h, _ := recipeWorld(t)
	scriptHits(h, karbonaderHits(t))
	r, err := h.s.Recipe(bg, "aftensmad/pastaretter/carbonara", nil)
	if err != nil {
		t.Fatal(err)
	}
	steps := r.Method[0].Steps
	if len(steps) == 0 || r.Tip == nil || strings.HasPrefix(*r.Tip, steps[len(steps)-1]) {
		t.Errorf("steps %v, tip %s", steps, show(r.Tip))
	}
	if show(r.Time) != "20 min" {
		t.Errorf("time %s", show(r.Time))
	}
}

func TestACategoryPageIsNotARecipe(t *testing.T) {
	h, _ := recipeWorld(t)
	_, err := h.s.Recipe(bg, "aftensmad", nil)
	if msg := storeErr(t, err); !strings.Contains(msg, "ikke en opskrift") {
		t.Errorf("message %q", msg)
	}
}

func TestFeaturedIsThisWeeksMadplan(t *testing.T) {
	h, _ := recipeWorld(t)
	week, err := h.s.FeaturedRecipes(bg, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(week) != 7 || week[0].Name != "Cowboy toast" || week[1].Name != "Carbonara" {
		t.Errorf("week: %d recipes, %v", len(week), week[:2])
	}
	if g := groupsOf(week); len(g) != 1 || !g["Madplan til uge 46"] {
		t.Errorf("groups %v", g)
	}
	if week[1].ID != "aftensmad/pastaretter/carbonara" {
		t.Errorf("id %q", week[1].ID)
	}
	if few, _ := h.s.FeaturedRecipes(bg, 3); len(few) != 3 {
		t.Errorf("limit 3 gave %d", len(few))
	}
}

func TestCategoryWithRecipes(t *testing.T) {
	h, _ := recipeWorld(t)
	category := "aftensmad/danske-retter"
	found, err := h.s.RecipesByTag(bg, nil, &category, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 15 || found[2].Name != "Karbonader" {
		t.Errorf("%d recipes, third %q", len(found), found[2].Name)
	}
	if g := groupsOf(found); len(g) != 1 || !g["Dansk inspirerede retter"] {
		t.Errorf("groups %v", g)
	}
}

func TestCategoryWithSubCategoriesMixesThem(t *testing.T) {
	h, w := recipeWorld(t)
	found, err := h.s.RecipesByTag(bg, nil, nil, 5) // aftensmad: 15 sub-categories
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 5 || !strings.HasPrefix(found[0].URL, "https://www.bilkatogo.dk/inspiration/") {
		t.Errorf("%d recipes, first %+v", len(found), found[0])
	}
	if !w.saw("/aftensmad/suppe/payload.js") { // the last one
		t.Error("the last sub-category was not read")
	}
}

func TestUnknownCategoryListsTheRealOnes(t *testing.T) {
	h, _ := recipeWorld(t)
	category := "pandekager"
	_, err := h.s.RecipesByTag(bg, nil, &category, 20)
	msg := storeErr(t, err)
	if !regexp.MustCompile(`kender ikke kategorien 'pandekager'.*aftensmad, frokost`).MatchString(msg) {
		t.Errorf("message %q", msg)
	}
	var np *noPageError
	if errors.As(err, &np) {
		t.Error("a NoPage must not leave the store")
	}
}

func TestTagsAndSearchSayWhatToUse(t *testing.T) {
	h, w := recipeWorld(t)
	_, err := h.s.RecipesByTag(bg, []string{"Familievenlig"}, nil, 20)
	if msg := storeErr(t, err); !strings.Contains(msg, "ingen tags") {
		t.Errorf("message %q", msg)
	}
	_, err = h.s.Recipes(bg, "karry", 5)
	if msg := storeErr(t, err); !strings.Contains(msg, "ingen opskriftssøgning") {
		t.Errorf("message %q", msg)
	}
	if len(w.paths) != 0 {
		t.Error("tags and search must not read anything")
	}
}

func TestNewBuildIDIsReadAgain(t *testing.T) {
	h, w := recipeWorld(t)
	if _, err := h.s.FeaturedRecipes(bg, 20); err != nil {
		t.Fatal(err)
	}
	w.setBuild("1800000000") // Bilka deployed
	if _, err := h.s.FeaturedRecipes(bg, 20); err != nil {
		t.Fatal(err)
	}
	if n := w.count("/inspiration/opskrifter/"); n != 2 {
		t.Errorf("%d reads of the build id, want 2", n)
	}
}

func TestNoBuildIDIsAClearError(t *testing.T) {
	h, w := recipeWorld(t)
	w.setBuild("nope") // the HTML has no build id
	_, err := h.s.FeaturedRecipes(bg, 20)
	if msg := storeErr(t, err); !strings.Contains(msg, "ændret format: ingen build") {
		t.Errorf("message %q", msg)
	}
}

// --- what the port adds -------------------------------------------------------------

func TestRecipeFromAnotherHostIsNeverRequested(t *testing.T) {
	h, w := recipeWorld(t)
	for _, link := range []string{"https://www.nemlig.com/maaltider/opskrift/kebabfad-98001303",
		"https://evil.example/www.bilkatogo.dk/aftensmad/pizza/flammkuchen"} {
		_, err := h.s.Recipe(bg, link, nil)
		if msg := storeErr(t, err); !strings.Contains(msg, "Kender ikke") {
			t.Errorf("%s: %q", link, msg)
		}
	}
	if len(w.paths) != 0 {
		t.Errorf("requests were made: %v", w.paths)
	}
}

func TestUnknownRecipePageIsAPlainStoreError(t *testing.T) {
	h, _ := recipeWorld(t)
	_, err := h.s.Recipe(bg, "frokost/findes-ikke", nil)
	if _, ok := err.(*store.StoreError); !ok {
		t.Fatalf("want a plain *store.StoreError, got %#v", err)
	}
	if err.Error() != "BilkaToGo har ingen side /inspiration/opskrifter/frokost/findes-ikke/." {
		t.Errorf("message %q", err)
	}
}

func TestRecipeWithoutAPersonCountHasNone(t *testing.T) {
	h, w := recipeWorld(t)
	w.override["aftensmad/danske-retter/karbonader"] = strings.Replace(recipeText("karbonader.payload.js"), `"4 personer"`, `"16 stk."`, 1)
	scriptHits(h, karbonaderHits(t))
	r, err := h.s.Recipe(bg, karbonader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Persons != nil || r.IngredientsPersons != nil || show(r.Time) != "35 min" {
		t.Errorf("persons %s / %s, time %s", show(r.Persons), show(r.IngredientsPersons), show(r.Time))
	}
}

func TestRecipeNeedsALoginForPrices(t *testing.T) {
	isolate(t)
	w := newSiteFake(newFake(t))
	s := New(&http.Client{Transport: w})
	_, err := s.Recipe(bg, karbonader, nil)
	if msg := storeErr(t, err); !strings.Contains(msg, "login") {
		t.Errorf("message %q", msg)
	}
	if _, err := s.FeaturedRecipes(bg, 20); err != nil { // the list needs no login
		t.Errorf("featured without a login: %v", err)
	}
}

func TestPageOf(t *testing.T) {
	for name, body := range map[string]string{
		"no data":      `__NUXT_JSONP__("/x", {fetch:{}});`,
		"data empty":   `__NUXT_JSONP__("/x", {data:[]});`,
		"data a text":  `__NUXT_JSONP__("/x", {data:"page"});`,
		"item a text":  `__NUXT_JSONP__("/x", {data:["page"]});`,
		"no page":      `__NUXT_JSONP__("/x", {data:[{}]});`,
		"page null":    `__NUXT_JSONP__("/x", {data:[{page:void 0}]});`,
		"page a list":  `__NUXT_JSONP__("/x", {data:[{page:[]}]});`,
		"cut off":      `__NUXT_JSONP__("/x", {data:[{page:{id:"1"`,
		"a function":   `__NUXT_JSONP__("/x", (function(a){return {data:[{page:{id:a}}]}}("1")));`,
		"a variable":   `__NUXT_JSONP__("/x", {data:[{page:{id:a}}]});`,
		"no object":    `__NUXT_JSONP__("/x")`,
		"extra tokens": `__NUXT_JSONP__("/x", {data:[{page:{}}]} x);`,
	} {
		if _, err := pageOf([]byte(body)); err == nil || err.Error() != formatChanged {
			t.Errorf("%s: %v", name, err)
		}
	}
	if p, err := pageOf([]byte(`__NUXT_JSONP__("/x", {data:[{page:{}}],fetch:{},mutations:[]});`)); err != nil || p == nil {
		t.Errorf("an empty page is a page: %v", err)
	}
}

// A broken page is one clear error from every recipe method, never a panic.
func TestMalformedPayloadIsAFormatChange(t *testing.T) {
	bodies := map[string]string{
		"cut off":     `__NUXT_JSONP__("/x", {data:[{page:{name:"a",childNodes:[{componentType:"MagnoliaComponentsHeading"`,
		"a variable":  `__NUXT_JSONP__("/x", {data:[{page:{name:a}}]});`,
		"deep":        `__NUXT_JSONP__("/x", {data:[{page:` + strings.Repeat("[", 5000) + strings.Repeat("]", 5000) + `}]});`,
		"bad escape":  `__NUXT_JSONP__("/x", {data:[{page:{name:"\q"}}]});`,
		"no data key": `__NUXT_JSONP__("/x", {});`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			h, w := recipeWorld(t)
			for _, page := range []string{"madplaner", "aftensmad", "aftensmad/pizza/flammkuchen"} {
				w.override[page] = body
			}
			category := "aftensmad"
			calls := map[string]func() error{
				"featured": func() error { _, err := h.s.FeaturedRecipes(bg, 5); return err },
				"category": func() error { _, err := h.s.RecipesByTag(bg, nil, &category, 5); return err },
				"recipe":   func() error { _, err := h.s.Recipe(bg, "aftensmad/pizza/flammkuchen", nil); return err },
			}
			for call, fn := range calls {
				if err := fn(); err == nil || err.Error() != formatChanged {
					t.Errorf("%s: %v", call, err)
				}
			}
		})
	}
}

// Pages with the right shape but odd values: no panic, and a clear answer.
func TestOddValuesInAPageDoNotBreakTheReader(t *testing.T) {
	h, w := recipeWorld(t)
	w.override["madplaner"] = `__NUXT_JSONP__("/x", {data:[{page:{path:7,componentType:"MagnoliaArea",childNodes:[
	  {componentType:5},{componentType:"MagnoliaComponentsContentCard",hierarchy:"x"},
	  {componentType:"MagnoliaComponentsCarouselContentCard",contentCards:[1,null,{page:3,header:9},{header:"A",page:{path:"/inspiration/opskrifter/../x"}},
	    {header:" Pasta ",page:{path:"/inspiration/opskrifter/aftensmad/pasta"}}]}]}}]});`
	week, err := h.s.FeaturedRecipes(bg, 20)
	if err != nil || len(week) != 1 || week[0].ID != "aftensmad/pasta" || week[0].Name != "Pasta" || week[0].Group != nil {
		t.Errorf("week %+v (%v)", week, err)
	}
}

func TestMixTakesOneFromEachGroupInTurn(t *testing.T) {
	r := func(ids ...string) (out []model.Recipe) {
		for _, id := range ids {
			out = append(out, model.Recipe{ID: id})
		}
		return out
	}
	var got []string
	for _, x := range mix([][]model.Recipe{r("a1", "a2", "a3"), r("b1"), nil, r("c1", "c2")}) {
		got = append(got, x.ID)
	}
	if want := []string{"a1", "b1", "c1", "a2", "c2", "a3"}; !slices.Equal(got, want) {
		t.Errorf("mix = %v, want %v", got, want)
	}
	if len(mix(nil)) != 0 {
		t.Error("no groups, no recipes")
	}
}
