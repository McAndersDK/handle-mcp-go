package nemlig

// Recipes from nemlig's new web API (productbff), found 2026-11-03 (tests/fixtures/nemlig/README.md).
// Read only: nemlig's own "add recipe to basket" is never called. The caller puts the products in
// the cart with set_cart_quantities, after the human says yes.

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

const site = base

// isSlug is SLUG.match: lower-case letters, digits and "-". (Python's "$" also let a trailing
// newline through; a newline in a URL path is refused here.)
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// recipeSlug turns a link ('https://www.nemlig.com/maaltider/opskrift/kebabfad-98001303?x=1', the
// old '/opskrifter/kebabfad-98001303' or just the slug) into the slug.
func recipeSlug(recipe string) (string, error) {
	s := pystr.Strip(recipe)
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = pystr.Lower(s)
	if !isSlug(s) {
		return "", store.Errorf("Kender ikke opskriften %s. Brug linket fra nemlig.com.", pystr.Repr(recipe))
	}
	return s, nil
}

// categoryPath turns 'Mad til flere dage' or 'måltidskasse' into nemlig's path.
func categoryPath(category string) (string, error) {
	p := strings.ReplaceAll(pystr.Lower(pystr.Strip(category)), " ", "-")
	for _, r := range [][2]string{{"æ", "ae"}, {"ø", "oe"}, {"å", "aa"}} {
		p = strings.ReplaceAll(p, r[0], r[1])
	}
	if !isSlug(p) {
		return "", store.Errorf("Kender ikke kategorien %s. Brug fx aftensmad eller frokost.", pystr.Repr(category))
	}
	return p, nil
}

// recipeProduct is _product(): a product picked for an ingredient. Prices are in øre. A product
// without a price is a format change, never a free product.
func recipeProduct(item obj) model.RecipeProduct {
	p := objOrEmpty(item["product"])
	qty := 1
	if v := item["quantity"]; truthy(v) {
		qty = toInt(v)
	}
	price := requiredMoney(field(p, "price"), "price").MulMoney(model.NewMoney(1, -2))
	out := model.NewRecipeProduct(pyStr(field(p, "id")), textOf(withDefault(p, "title", "")), price, qty)
	out.Size = optStr(p["description"])
	out.Available = withDefault(objOrEmpty(p["availability"]), "type", "Available") == "Available"
	for _, c := range listOrEmpty(p["campaignLines"]) {
		if text := asObj(c)["text"]; truthy(text) {
			out.OfferText = strPtr(pyStr(text))
			break
		}
	}
	return out
}

func recipeItems(rows []any, haveAtHome bool) []model.RecipeItem {
	var items []model.RecipeItem
	for _, r := range rows {
		row := asObj(r)
		item := objOrEmpty(row["item"])
		if !truthy(asObj(withDefault(item, "product", obj{}))["id"]) {
			continue
		}
		ingredient := textOf(withDefault(row, "ingredientId", ""))
		if t := row["ingredientTitle"]; truthy(t) {
			ingredient = pyStr(t)
		}
		it := model.RecipeItem{Ingredient: ingredient, Product: recipeProduct(asObj(field(row, "item"))),
			HaveAtHome: haveAtHome, Alternatives: model.List[model.RecipeProduct]{}}
		for _, a := range listOrEmpty(row["alternatives"]) {
			if alt := asObj(a); truthy(alt["product"]) {
				it.Alternatives = append(it.Alternatives, recipeProduct(alt))
			}
		}
		items = append(items, it)
	}
	return items
}

// text is _text(): a stripped text, or nil.
func text(v any) *string {
	s, ok := v.(string)
	if !ok || pystr.Strip(s) == "" {
		return nil
	}
	return strPtr(pystr.Strip(s))
}

// softInt is _int(): a count the store may send as "4", 4 or junk. Junk and 0 are nil.
func softInt(v any) (out *int) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(shapeError); !ok {
				panic(r)
			}
			out = nil
		}
	}()
	if n := toInt(v); n != 0 {
		return &n
	}
	return nil
}

func method(details obj) model.List[model.RecipeSection] {
	sections := model.List[model.RecipeSection]{}
	for _, g := range listOrEmpty(details["instructions"]) {
		group := asObj(g)
		steps := model.List[string]{}
		for _, st := range listOrEmpty(group["steps"]) {
			if t := text(asObj(st)["description"]); t != nil {
				steps = append(steps, *t)
			}
		}
		if len(steps) > 0 {
			sections = append(sections, model.RecipeSection{Title: text(group["title"]), Steps: steps})
		}
	}
	return sections
}

// image is the dish's photo: the first https image. The byline's portrait is not the dish.
func image(details obj) *string {
	for _, i := range listOrEmpty(details["images"]) {
		if s := text(asObj(i)["source"]); s != nil && strings.HasPrefix(*s, "https://") {
			return s
		}
	}
	return nil
}

func optInt(v any) *int {
	if v == nil {
		return nil
	}
	n := toInt(v)
	return &n
}

// Recipe is a recipe with its method and the products to buy for `persons`.
func (s *Store) Recipe(ctx context.Context, recipeID string, persons *int) (_ model.RecipeDetails, err error) {
	defer guardAs(&err, "nemlig-opskriften har ændret format.")
	slug, err := recipeSlug(recipeID)
	if err != nil {
		return model.RecipeDetails{}, err
	}
	p := params{}.add("recipeId", slug).add("maxProductsPerIngredient", 10)
	slot, err := s.timeslotID(ctx)
	if err != nil {
		return model.RecipeDetails{}, err
	}
	if slot != "" {
		p = p.add("timeSlotId", slot)
	}
	if persons != nil && *persons != 0 {
		p = p.add("servings", *persons)
	}
	var detailsRaw, dialogRaw any
	var detailsErr, dialogErr error
	var wg sync.WaitGroup
	wg.Add(2) // the two calls are independent: ask for both at once
	go func() {
		defer wg.Done()
		detailsRaw, detailsErr = s.api(ctx, call{method: "GET", url: bff + "/meals/recipeDetails/" + slug})
	}()
	go func() {
		defer wg.Done()
		dialogRaw, dialogErr = s.api(ctx, call{method: "GET", url: bff + "/meals/recipeProductDialog", params: p})
	}()
	wg.Wait()
	if detailsErr != nil {
		return model.RecipeDetails{}, detailsErr
	}
	if dialogErr != nil {
		return model.RecipeDetails{}, dialogErr
	}
	details, dialog := objOrEmpty(detailsRaw), objOrEmpty(dialogRaw)
	if !has(dialog, "includedProducts") {
		return model.RecipeDetails{}, store.Errorf("nemlig-opskriften har ændret format.")
	}
	items := recipeItems(listOrEmpty(dialog["includedProducts"]), false)
	items = append(items, recipeItems(listOrEmpty(dialog["selfSuppliedProducts"]), true)...)
	limitsRaw := dialog["servingLimits"]
	if !truthy(limitsRaw) {
		limitsRaw = details["servingLimits"]
	}
	limits := objOrEmpty(limitsRaw)

	r := model.RecipeDetails{
		Store: name, ID: slug, URL: site + "/maaltider/opskrift/" + slug,
		MinPersons: optInt(limits["minServings"]), MaxPersons: optInt(limits["maxServings"]),
		Description: text(details["description"]), Image: image(details), Tip: text(details["tip"]),
		// recipeDetails has no servings parameter: its text is always for the recipe's own servings,
		// while the products follow `persons` (live, 2026-11-05).
		IngredientsPersons: softInt(details["servings"]),
		Method:             method(details), Ingredients: model.List[string]{}, Items: model.List[model.RecipeItem](items),
	}
	if title := dialog["title"]; truthy(title) {
		r.Name = pyStr(title)
	} else {
		r.Name = textOf(withDefault(details, "title", ""))
	}
	switch {
	case softInt(dialog["servings"]) != nil:
		r.Persons = *softInt(dialog["servings"])
	case softInt(details["servings"]) != nil:
		r.Persons = *softInt(details["servings"])
	case persons != nil:
		r.Persons = *persons
	}
	if minutes := objOrEmpty(details["durationTotal"])["from"]; truthy(minutes) {
		r.Time = strPtr(fmt.Sprintf("%s min", pyStr(minutes)))
	}
	for _, g := range listOrEmpty(details["ingredients"]) {
		for _, i := range listOrEmpty(asObj(g)["ingredients"]) {
			if t := asObj(i)["text"]; truthy(t) {
				r.Ingredients = append(r.Ingredients, pyStr(t))
			}
		}
	}
	total := model.Money{}
	for _, it := range items {
		if !it.HaveAtHome {
			total = total.Add(it.Product.Price.Mul(int64(it.Product.Quantity)))
		}
	}
	r.Total = total
	return r, nil
}

// Recipes is nemlig's recipe search, as on the website. The old search (searchgateway) also finds
// recipes that the new API no longer has (500 on get_recipe, live 2026-11-03), so not that. Name
// and link only: Recipe gives time, persons and products.
func (s *Store) Recipes(ctx context.Context, query string, limit int) (_ []model.Recipe, err error) {
	defer guardAs(&err, "nemlig-opskrifter har ændret format.")
	p := params{}.add("query", query).add("take", limit).add("skip", 0)
	slot, err := s.timeslotID(ctx)
	if err != nil {
		return nil, err
	}
	if slot != "" {
		p = p.add("timeslotId", slot)
	}
	data, err := s.api(ctx, call{method: "GET", url: bff + "/search/recipes", params: p})
	if err != nil {
		return nil, err
	}
	hits, ok := objOrEmpty(objOrEmpty(data)["searchResult"])["items"].([]any)
	if !ok {
		return nil, store.Errorf("nemlig-opskrifter har ændret format.")
	}
	out := []model.Recipe{}
	for _, h := range sliceTo(hits, limit) {
		if hit := asObj(h); truthy(hit["id"]) {
			out = append(out, recipeOf(hit, nil))
		}
	}
	return out, nil
}

func recipeOf(r obj, group *string) model.Recipe {
	id := pyStr(field(r, "id"))
	return model.Recipe{Store: name, ID: id, Name: textOf(withDefault(r, "title", "")),
		URL: site + "/maaltider/opskrift/" + id, Group: group}
}

// FeaturedRecipes is nemlig's "Ugens udvalgte" (/maaltider), grouped by theme.
func (s *Store) FeaturedRecipes(ctx context.Context, limit int) (_ []model.Recipe, err error) {
	defer guardAs(&err, "nemlig-opskrifter har ændret format.")
	data, err := s.api(ctx, call{method: "GET", url: bff + "/page", params: params{}.add("path", "/maaltider")})
	if err != nil {
		return nil, err
	}
	var blocks []obj
	for _, b := range listOrEmpty(objOrEmpty(data)["pageContent"]) {
		if block := asObj(b); has(block, "recipes") {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return nil, store.Errorf("Fandt ingen opskrifter i nemligs 'Ugens udvalgte'.")
	}
	out := []model.Recipe{}
	for _, b := range blocks {
		group := rawStr(objOrEmpty(b["header"])["title"])
		for _, r := range listOrEmpty(b["recipes"]) {
			if rec := asObj(r); truthy(rec["id"]) {
				out = append(out, recipeOf(rec, group))
			}
		}
	}
	return sliceTo(out, limit), nil
}

// RecipesByTag are all of nemlig's recipes with every tag, e.g. ["Familievenlig", "Under 30 min"].
// Tags are matched on nemlig's own list, by title or path. Category defaults to nemlig's
// "aftensmad".
func (s *Store) RecipesByTag(ctx context.Context, tags []string, category *string, limit int) (_ []model.Recipe, err error) {
	defer guardAs(&err, "nemlig-opskrifter har ændret format.")
	known, names, err := s.recipeTags(ctx)
	if err != nil {
		return nil, err
	}
	p := params{}.add("skip", 0).add("take", limit)
	for _, tag := range tags {
		path, ok := known[pystr.Lower(pystr.Strip(tag))]
		if !ok || path == "" {
			return nil, store.Errorf("nemlig kender ikke tagget %s. Brug et af: %s.", pystr.Repr(tag), strings.Join(names, ", "))
		}
		p = p.add("filters", path)
	}
	if category != nil && *category != "" {
		path, err := categoryPath(*category)
		if err != nil {
			return nil, err
		}
		p = p.add("categoryPath", path)
	}
	data, err := s.api(ctx, call{method: "GET", url: bff + "/meals/allRecipes", params: p})
	if err != nil {
		return nil, err
	}
	found := objOrEmpty(objOrEmpty(data)["requestedCategory"])
	list, ok := found["recipes"].([]any)
	if !ok {
		return nil, store.Errorf("nemlig-opskrifter har ændret format.")
	}
	group := rawStr(found["title"])
	out := []model.Recipe{}
	for _, r := range list {
		if rec := asObj(r); truthy(rec["id"]) {
			out = append(out, recipeOf(rec, group))
		}
	}
	return sliceTo(out, limit), nil
}

// recipeTags are nemlig's tags, by lower-case title and by path, to path. Cached: they rarely
// change.
func (s *Store) recipeTags(ctx context.Context) (map[string]string, []string, error) {
	s.mu.Lock()
	tags, names := s.tags, s.tagNames
	s.mu.Unlock()
	if len(tags) > 0 {
		return tags, names, nil
	}
	data, err := s.api(ctx, call{method: "GET", url: bff + "/meals/allRecipes", params: params{}.add("skip", 0).add("take", 1)})
	if err != nil {
		return nil, nil, err
	}
	var found []obj
	for _, g := range listOrEmpty(objOrEmpty(data)["filters"]) {
		for _, f := range listOrEmpty(asObj(g)["filters"]) {
			if filter := asObj(f); truthy(filter["path"]) {
				found = append(found, filter)
			}
		}
	}
	if len(found) == 0 {
		return nil, nil, store.Errorf("nemlig-opskrifter har ændret format: ingen tags.")
	}
	tags, names = map[string]string{}, nil
	for _, f := range found {
		path := pyStr(f["path"])
		title := path
		if t := f["title"]; truthy(t) {
			title = pyStr(t)
		}
		names = append(names, pystr.Strip(title))
		for _, key := range []string{textOf(withDefault(f, "title", "")), path} {
			if pystr.Strip(key) != "" {
				tags[pystr.Lower(pystr.Strip(key))] = path
			}
		}
	}
	s.mu.Lock()
	s.tags, s.tagNames = tags, names
	s.mu.Unlock()
	return tags, names, nil
}
