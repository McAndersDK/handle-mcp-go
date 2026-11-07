package compare_test

// The delivery mode in compare (docs/adr/0006): the quote copies the mode and the pickup store,
// says so in a note, and the verdict names the cheapest store for each mode when the stores differ.

import (
	"context"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

const eksempelbyStore = "Bilka Eksempelby"

func modeTerms(t model.StoreTerms, mode string, pickupStore string) model.StoreTerms {
	if mode != "" {
		t.DeliveryMode = ptr(mode)
	}
	if pickupStore != "" {
		t.PickupStore = ptr(pickupStore)
	}
	return t
}

func TestQuoteCopiesModeAndStoreAndNotesPickup(t *testing.T) {
	one := []string{"rugbrød"}
	cases := []struct {
		name      string
		terms     model.StoreTerms
		mode      string // "-" is nil
		storeName string
		note      string // "" means no mode note
	}{
		{"pickup with store", modeTerms(pickup, "pickup", eksempelbyStore), "pickup", eksempelbyStore,
			"Afhentning i Bilka Eksempelby: du henter selv varerne."},
		{"pickup without store", modeTerms(pickup, "pickup", ""), "pickup", "-",
			"Afhentning: butikken er ikke oplyst."},
		{"unknown mode keeps the old note", pickup, "-", "-", "Kurven er ikke sat til hjemlevering."},
		{"home has no mode note", modeTerms(termsB, "home", ""), "home", "-", ""},
	}
	for _, c := range cases {
		q, err := compare.Quote(context.Background(), &fake{catalog: bilkaCatalog, terms: c.terms}, one)
		if err != nil {
			t.Fatal(err)
		}
		gotStore := "-"
		if q.PickupStore != nil {
			gotStore = *q.PickupStore
		}
		gotMode := "-"
		if q.DeliveryMode != nil {
			gotMode = *q.DeliveryMode
		}
		if gotMode != c.mode || gotStore != c.storeName {
			t.Errorf("%s: mode %s store %s", c.name, gotMode, gotStore)
		}
		var mode []string
		for _, n := range q.Notes {
			if strings.HasPrefix(n, "Afhentning") || strings.HasPrefix(n, "Kurven er ikke") {
				mode = append(mode, n)
			}
		}
		if (c.note == "") != (len(mode) == 0) || (c.note != "" && (len(mode) != 1 || mode[0] != c.note)) {
			t.Errorf("%s: mode notes %q, want %q", c.name, mode, c.note)
		}
	}
}

// A store with a note of its own and a known pickup keeps both; the old not-home note is only for
// an unknown mode.
func TestPickupNoteComesAfterTheStoresOwnNote(t *testing.T) {
	f := &fake{catalog: bilkaCatalog, terms: modeTerms(pickup, "pickup", eksempelbyStore), info: model.DeliveryInfo{Note: ptr("Egen note.")}}
	q, err := compare.Quote(context.Background(), f, []string{"rugbrød"})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Notes) != 2 || q.Notes[0] != "Egen note." || q.Notes[1] != "Afhentning i Bilka Eksempelby: du henter selv varerne." {
		t.Errorf("notes %q", q.Notes)
	}
}

func verdictOf(t *testing.T, stores map[string]*fake, items []string) compare.VerdictAnswer {
	t.Helper()
	var targets []compare.Target
	for _, n := range []string{"bilkatogo", "nemlig", "rema"} { // sorted, as the server does
		if f, ok := stores[n]; ok {
			targets = append(targets, compare.Target{Name: n, Store: f})
		}
	}
	results := compare.Gather(context.Background(), targets, func(ctx context.Context, s store.Store) (compare.BasketQuote, error) {
		return compare.Quote(ctx, s, items)
	})
	return compare.Verdict(results)
}

func TestVerdictNamesTheCheapestPerModeWhenTheyDiffer(t *testing.T) {
	items := []string{"rugbrød", "smør 250 g"}
	home := &fake{catalog: nemligCatalog, terms: modeTerms(termsN, "home", "")}
	pick := &fake{catalog: bilkaCatalog, terms: modeTerms(termsB, "pickup", eksempelbyStore)}
	v := verdictOf(t, map[string]*fake{"nemlig": home, "bilkatogo": pick}, items)
	nem, bil := v.ComparableTotals["nemlig"], v.ComparableTotals["bilkatogo"]
	want := " Billigst med levering: nemlig (" + nem.String() + " kr.). Billigst med afhentning: bilkatogo (" + bil.String() +
		" kr.). Turen til butikken er ikke regnet med."
	if !strings.HasSuffix(v.Summary, want) {
		t.Errorf("summary %q\nwant the end %q", v.Summary, want)
	}
	if !strings.HasPrefix(v.Summary, "Billigst: ") || v.Cheapest == nil {
		t.Errorf("the old summary stays first: %q", v.Summary)
	}
	// cheapest is money only: the same store as without the modes
	plain := verdictOf(t, map[string]*fake{
		"nemlig":    {catalog: nemligCatalog, terms: termsN},
		"bilkatogo": {catalog: bilkaCatalog, terms: termsB},
	}, items)
	if *v.Cheapest != *plain.Cheapest {
		t.Errorf("cheapest %s changed to %s because of the modes", *plain.Cheapest, *v.Cheapest)
	}
}

func TestVerdictPicksTheCheapestOfEachModeAmongThree(t *testing.T) {
	items := []string{"rugbrød"}
	cheapPickup := modeTerms(terms("rema", false, "", nil, ""), "pickup", "Rema Eksempelby")
	v := verdictOf(t, map[string]*fake{
		"nemlig":    {catalog: nemligCatalog, terms: modeTerms(termsN, "home", "")},
		"bilkatogo": {catalog: bilkaCatalog, terms: modeTerms(termsB, "pickup", eksempelbyStore)},
		"rema":      {catalog: bilkaCatalog, terms: cheapPickup},
	}, items)
	// bilkatogo has a 39 kr fee on top; the same catalog at rema has none: rema is the cheaper pickup
	if !strings.Contains(v.Summary, "Billigst med afhentning: rema (") || !strings.Contains(v.Summary, "Billigst med levering: nemlig (") {
		t.Errorf("summary %q", v.Summary)
	}
}

func TestVerdictHasNoModeSentenceWithOneModeOrAnUnknownOne(t *testing.T) {
	items := []string{"rugbrød"}
	home := func(c []model.Product, t model.StoreTerms) *fake {
		return &fake{catalog: c, terms: modeTerms(t, "home", "")}
	}
	pick := func(c []model.Product, t model.StoreTerms) *fake {
		return &fake{catalog: c, terms: modeTerms(t, "pickup", eksempelbyStore)}
	}
	unknown := func(c []model.Product, t model.StoreTerms) *fake { return &fake{catalog: c, terms: t} }
	for _, c := range []struct {
		name string
		a, b *fake
	}{
		{"all home", home(nemligCatalog, termsN), home(bilkaCatalog, termsB)},
		{"all pickup", pick(nemligCatalog, termsN), pick(bilkaCatalog, termsB)},
		{"one unknown", home(nemligCatalog, termsN), unknown(bilkaCatalog, pickup)},
		{"the other unknown", unknown(nemligCatalog, termsN), pick(bilkaCatalog, termsB)},
		{"both unknown", unknown(nemligCatalog, termsN), unknown(bilkaCatalog, termsB)},
	} {
		v := verdictOf(t, map[string]*fake{"nemlig": c.a, "bilkatogo": c.b}, items)
		if strings.Contains(v.Summary, "Billigst med") || strings.Contains(v.Summary, "Turen til butikken") {
			t.Errorf("%s: a mode sentence where there should be none: %q", c.name, v.Summary)
		}
	}
}
