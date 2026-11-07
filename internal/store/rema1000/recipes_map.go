package rema1000

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// apiRecipe is a recipe as the public API sends it. Only the fields an answer needs.
type apiRecipe struct {
	Fields struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
		Image *struct {
			URL string `json:"url"`
		} `json:"image"`
		Tags       []apiTag `json:"tags"`
		CustomForm struct {
			Servings *struct {
				Type   string      `json:"type"`
				Amount json.Number `json:"amount"`
			} `json:"servings"`
			TotalTime        json.Number `json:"totalTime"`
			IngredientGroups []struct {
				Ingredients []apiIngredient `json:"ingredients"`
			} `json:"ingredientGroups"`
			PreparationGroups []struct {
				Name  string `json:"name"`
				Steps []struct {
					Step string `json:"step"`
				} `json:"steps"`
			} `json:"preparationGroups"`
			TextBefore string `json:"preparationTextBefore"`
			TextAfter  string `json:"preparationTextAfter"`
		} `json:"customForm"`
	} `json:"fields"`
}

// apiIngredient is one ingredient line. digitalProduct says which product REMA picked for it and
// how many packs the recipe needs at its own number of persons.
type apiIngredient struct {
	Ingredient string      `json:"ingredient"`
	Unit       string      `json:"unitOfContent"`
	Amount     json.Number `json:"amountOfContent"`
	Excluded   bool        `json:"isExcludedFromShoppingList"`
	Product    *struct {
		Packs  json.Number `json:"amount"`
		Basic  bool        `json:"is_basic_ingredient"`
		Active *apiProduct `json:"active_product"`
	} `json:"digitalProduct"`
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// persons is the number of persons the recipe is written for; nil when it is written for pieces.
func (r apiRecipe) persons() *int {
	sv := r.Fields.CustomForm.Servings
	if sv == nil || sv.Type != "person" {
		return nil
	}
	if n, err := strconv.Atoi(string(sv.Amount)); err == nil && n > 0 {
		return &n
	}
	return nil
}

func (r apiRecipe) minutes() *string {
	if n, err := strconv.Atoi(string(r.Fields.CustomForm.TotalTime)); err == nil && n > 0 {
		return strPtr(fmt.Sprintf("%d min", n))
	}
	return nil
}

func (r apiRecipe) hasTags(ids []string) bool {
	for _, id := range ids {
		found := false
		for _, t := range r.Fields.Tags {
			found = found || t.ID == id
		}
		if !found {
			return false
		}
	}
	return true
}

func (r apiRecipe) summary() model.Recipe {
	out := model.Recipe{Store: name, ID: r.Fields.Slug, Name: strings.TrimSpace(r.Fields.Title),
		URL: siteRecipes + r.Fields.Slug, Time: r.minutes(), Persons: r.persons()}
	if len(r.Fields.Tags) > 0 {
		out.Group = text(r.Fields.Tags[0].Fields.Title)
	}
	return out
}

// amountText writes 400 as "400" and 1.5 as "1,5", as a Danish recipe does.
func amountText(n json.Number) string {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return ""
	}
	return strings.Replace(strconv.FormatFloat(f, 'f', -1, 64), ".", ",", 1)
}

// details maps a recipe. Products are REMA's pick per ingredient. The packs are scaled to
// `persons` when both it and the recipe's own persons are known, and rounded up.
func (s *Store) details(r apiRecipe, persons *int) (model.RecipeDetails, error) {
	own := r.persons()
	factor := 1.0
	shown := own
	if own != nil && persons != nil && *persons > 0 {
		factor, shown = float64(*persons)/float64(*own), persons
	}
	today := s.today()
	d := model.RecipeDetails{Store: name, ID: r.Fields.Slug, Name: strings.TrimSpace(r.Fields.Title),
		URL: siteRecipes + r.Fields.Slug, Persons: shown, Time: r.minutes(), IngredientsPersons: own,
		Description: text(htmlTag.ReplaceAllString(r.Fields.CustomForm.TextBefore, "")),
		Tip:         text(htmlTag.ReplaceAllString(r.Fields.CustomForm.TextAfter, "")),
		Ingredients: model.List[string]{}, Method: model.List[model.RecipeSection]{},
		Items: model.List[model.RecipeItem]{}, Missing: model.List[string]{}}
	if r.Fields.Image != nil {
		d.Image = text(r.Fields.Image.URL)
	}
	for _, g := range r.Fields.CustomForm.IngredientGroups {
		for _, ing := range g.Ingredients {
			d.Ingredients = append(d.Ingredients, strings.TrimSpace(
				strings.Join(nonEmpty(amountText(ing.Amount), ing.Unit, ing.Ingredient), " ")))
			if ing.Product == nil || ing.Product.Active == nil {
				if !ing.Excluded {
					d.Missing = append(d.Missing, strings.TrimSpace(ing.Ingredient))
				}
				continue
			}
			p, ok, err := toProduct(*ing.Product.Active, today)
			if err != nil {
				return model.RecipeDetails{}, err
			}
			if !ok {
				d.Missing = append(d.Missing, strings.TrimSpace(ing.Ingredient))
				continue
			}
			packs := packsFor(ing.Product.Packs, factor)
			rp := model.NewRecipeProduct(p.ID, p.Name, p.Price, packs)
			rp.Size, rp.OfferText = p.Size, p.OfferText
			item := model.RecipeItem{Ingredient: strings.TrimSpace(ing.Ingredient), Product: rp,
				HaveAtHome:   ing.Excluded || ing.Product.Basic, // pantry items: left out of the total
				Alternatives: model.List[model.RecipeProduct]{}}
			d.Items = append(d.Items, item)
			if !item.HaveAtHome {
				d.Total = d.Total.Add(p.Price.Mul(int64(packs)))
			}
		}
	}
	for _, g := range r.Fields.CustomForm.PreparationGroups {
		sec := model.RecipeSection{Title: text(g.Name), Steps: model.List[string]{}}
		for _, st := range g.Steps {
			if t := strings.TrimSpace(htmlTag.ReplaceAllString(st.Step, "")); t != "" {
				sec.Steps = append(sec.Steps, t)
			}
		}
		if len(sec.Steps) > 0 {
			d.Method = append(d.Method, sec)
		}
	}
	return d, nil
}

// packsFor is the packs the recipe needs for the persons asked for: at least one, rounded up.
func packsFor(n json.Number, factor float64) int {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || f <= 0 {
		f = 1
	}
	return max(1, int(math.Ceil(f*factor-1e-9)))
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}
