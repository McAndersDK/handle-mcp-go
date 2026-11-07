package bilkatogo

import (
	"context"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// Recipe is one recipe and its products, one pack each. `persons` is not used: BilkaToGo's recipes
// are written for a fixed number of persons and have no amounts per product. Prices come from the
// store behind the cart's delivery address, so this needs a login.
func (s *Store) Recipe(ctx context.Context, recipeID string, _ *int) (_ model.RecipeDetails, err error) {
	defer recipeGuard(&err)
	path, err := recipePath(recipeID)
	if err != nil {
		return model.RecipeDetails{}, err
	}
	page, err := s.page(ctx, path)
	if err != nil {
		return model.RecipeDetails{}, err
	}
	secs := readSections(page)
	ingredients := secs.first("Ingrediens")
	skus := itemNumbers(page)
	if len(ingredients) == 0 && len(skus) == 0 {
		return model.RecipeDetails{}, store.Errorf("%s%s/ er ikke en opskrift hos BilkaToGo.", pageRoot, path)
	}
	found := map[string]model.Product{}
	if len(skus) > 0 {
		hits, err := s.algolia(ctx, "", len(skus), false, skus)
		if err != nil {
			return model.RecipeDetails{}, err
		}
		for _, p := range hits {
			found[p.ID] = p
		}
		if len(found) == 0 { // the lookup by item number itself failed, or nothing is sold here
			return model.RecipeDetails{}, store.Errorf("BilkaToGo fandt ingen af opskriftens varer i din butik.")
		}
	}
	items := model.List[model.RecipeItem]{}
	missing := model.List[string]{}
	total := model.Money{}
	for _, sku := range skus {
		p, ok := found[sku]
		if !ok {
			missing = append(missing, sku)
			continue
		}
		prod := model.NewRecipeProduct(p.ID, p.Name, p.Price, 1)
		prod.Size, prod.Available, prod.OfferText = p.Size, p.Available, p.OfferText
		items = append(items, model.RecipeItem{Ingredient: p.Name, Product: prod})
		total = total.Add(p.Price)
	}
	own, minutes := facts(page)
	title := ""
	if headings := nodes(page, "Heading"); len(headings) > 0 {
		title = headings[0].text("header")
	}
	if title == "" {
		title = nameOf(page)
	}
	r := model.RecipeDetails{
		Store: name, ID: path, Name: pystr.Strip(title), URL: siteURL + pageRoot + path + "/",
		Time: minutes, Description: joined(secs.at("").lines), Image: image(page),
		Ingredients: model.List[string](ingredients), Tip: joined(secs.first("Alternativer")),
		Items: items, Total: total, Missing: missing,
	}
	if own != nil { // written for "6 stk" or "1 glas", not persons: both stay null
		persons, forText := *own, *own
		r.Persons, r.IngredientsPersons = &persons, &forText
	}
	if steps := secs.first(methodHds...); len(steps) > 0 {
		r.Method = model.List[model.RecipeSection]{{Steps: model.List[string](steps)}}
	}
	return r, nil
}

// joined is " ".join(lines) or None.
func joined(lines []string) *string {
	if s := strings.Join(lines, " "); s != "" {
		return &s
	}
	return nil
}
