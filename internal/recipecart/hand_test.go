package recipecart_test

import (
	"errors"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/recipecart"
)

func recipe() model.RecipeDetails {
	p := func(id, name string, qty int) model.RecipeProduct {
		return model.NewRecipeProduct(id, name, model.NewMoney(10, 0), qty)
	}
	return model.RecipeDetails{Items: model.List[model.RecipeItem]{
		{Ingredient: "Løg", Product: p("10", "Løg", 2), Alternatives: model.List[model.RecipeProduct]{p("11", "Rødløg", 1)}},
		{Ingredient: "Rødløg", Product: p("10", "Løg", 3)},
		{Ingredient: "Salt", Product: p("20", "Salt", 1), HaveAtHome: true},
	}}
}

func TestPlanAddsUpAndKeepsOrder(t *testing.T) {
	lines, skipped, err := recipecart.Plan(recipe(), nil, nil, nil)
	if err != nil || len(lines) != 1 || lines[0].Quantity != 5 || len(lines[0].Ingredients) != 2 || len(skipped) != 1 {
		t.Fatalf("%v %+v %+v", err, lines, skipped)
	}
}

func TestPlanKeysAreStrippedAndLowered(t *testing.T) {
	// a non-breaking space and upper case Danish letters around the key
	lines, skipped, err := recipecart.Plan(recipe(), []string{" LØG "}, nil, nil)
	if err != nil || len(lines) != 1 || lines[0].Ingredients[0] != "Rødløg" || len(skipped) != 2 {
		t.Fatalf("%v %+v %+v", err, lines, skipped)
	}
}

func TestPlanSwapRules(t *testing.T) {
	// an empty swap value swaps nothing, and the first key that fits an item decides
	lines, _, err := recipecart.Plan(recipe(), nil, []recipecart.Swap{{Key: "løg", Value: ""}, {Key: "Løg", Value: "11"}}, nil)
	if err != nil || len(lines) != 1 || lines[0].ProductID != "10" {
		t.Fatalf("%v %+v", err, lines)
	}
	_, _, err = recipecart.Plan(recipe(), nil, []recipecart.Swap{{Key: "Løg", Value: "99"}}, nil)
	var pe *recipecart.PlanError
	if !errors.As(err, &pe) || pe.Msg != "99 er ikke et alternativ til Løg: 11 Rødløg." {
		t.Errorf("%v", err)
	}
	_, _, err = recipecart.Plan(recipe(), []string{"it's"}, nil, nil)
	if !errors.As(err, &pe) || pe.Msg != `Opskriften har ikke "it's". Ingredienser: Løg, Rødløg, Salt.` {
		t.Errorf("%v", err)
	}
}
