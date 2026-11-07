package privacy_test

import (
	"reflect"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

// modelTypes are every struct that can reach an answer. The server phase adds its own
// answer types here (or calls the same two helpers from its own test).
var modelTypes = []reflect.Type{
	reflect.TypeFor[model.Product](), reflect.TypeFor[model.Recipe](),
	reflect.TypeFor[model.RecipeDetails](), reflect.TypeFor[model.Cart](),
	reflect.TypeFor[model.QuantityResult](), reflect.TypeFor[model.StoreTerms](),
	reflect.TypeFor[model.DeliveryInfo](), reflect.TypeFor[model.DeliverySlot](),
	reflect.TypeFor[model.Order](),
}

// A new field fails here until it is added to PassthroughKeys, or is a free-text or
// personal key that Scrub already knows.
func TestEveryModelKeyIsClassified(t *testing.T) {
	if keys := privacy.UnclassifiedKeys(modelTypes...); len(keys) > 0 {
		t.Errorf("unclassified model keys: %v (add to privacy.PassthroughKeys if harmless)", keys)
	}
}

func TestNoModelKeyIsPersonal(t *testing.T) {
	if keys := privacy.PersonalKeysOf(modelTypes...); len(keys) > 0 {
		t.Errorf("personal keys on models: %v", keys)
	}
}

func TestFreeTextModelKeys(t *testing.T) {
	var free []string
	for _, k := range privacy.JSONKeys(modelTypes...) {
		if privacy.Classify(k) == privacy.FreeText {
			free = append(free, k)
		}
	}
	if want := []string{"message", "note", "warnings"}; !reflect.DeepEqual(free, want) {
		t.Errorf("free-text model keys %v, want %v", free, want)
	}
	if n := len(privacy.JSONKeys(modelTypes...)); n < 60 {
		t.Errorf("only %d keys found: the walker misses fields", n)
	}
}

func TestClassifyUnknownKeyIsUnclassified(t *testing.T) {
	if privacy.Classify("surprise") != privacy.Unclassified {
		t.Error("unknown key must stay unclassified")
	}
	if privacy.Classify("Door_Code") != privacy.Personal {
		t.Error("door_code is personal in any spelling")
	}
}
