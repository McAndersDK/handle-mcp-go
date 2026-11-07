package compare_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func TestGatherKeepsOrderAndSurvivesAPanic(t *testing.T) {
	targets := []compare.Target{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	boom := errors.New("boom")
	names := []string{"a", "b", "c", "d"}
	for i, tg := range targets {
		targets[i].Store = &fake{name: tg.Name}
	}
	res := compare.Gather(context.Background(), targets, func(_ context.Context, s store.Store) (string, error) {
		switch s.Name() {
		case "b":
			panic("kaboom")
		case "c":
			return "", boom
		}
		return s.Name() + "!", nil
	})
	for i, r := range res {
		if r.Name != names[i] {
			t.Errorf("result %d is %s, want %s", i, r.Name, names[i])
		}
	}
	var pe *compare.PanicError
	if !errors.As(res[1].Err, &pe) || pe.Value != "kaboom" {
		t.Errorf("b: %v", res[1].Err)
	}
	if res[2].Err != boom || res[0].Value != "a!" || res[3].Value != "d!" || res[0].Err != nil {
		t.Errorf("others: %+v", res)
	}
}

func TestVerdictFirstOfEqualsWins(t *testing.T) {
	line := func(item string, total string) compare.BasketLine {
		p := model.NewProduct("x", "1", item, money(total))
		return compare.BasketLine{Item: item, Product: &p, LineTotal: money(total), Sure: true}
	}
	quote := func(total string) compare.BasketQuote {
		return compare.BasketQuote{Lines: model.List[compare.BasketLine]{line("mælk", total)}}
	}
	got := compare.Verdict([]compare.Result[compare.BasketQuote]{
		{Name: "z", Value: quote("10")}, {Name: "a", Value: quote("10.00")}, {Name: "m", Value: quote("12")},
	})
	if got.Cheapest == nil || *got.Cheapest != "z" || got.Summary != "Billigst: z (10.00 kr. inkl. gebyrer for de 1 varer, alle butikker har)." {
		t.Errorf("%+v", got)
	}
	one := compare.Verdict([]compare.Result[compare.BasketQuote]{{Name: "a", Value: quote("1")}, {Name: "b", Err: errors.New("x")}})
	if one.Cheapest != nil || one.Summary != "Kun a kunne prise listen. Fejl: b." {
		t.Errorf("%+v", one)
	}
}

func TestDeliveryDayIsADanishDay(t *testing.T) {
	// 23:30 UTC on 2026-11-04 is 00:30 on the 5th in Copenhagen: an offer ending the 4th is over
	p := prod("x", "1", "Mælk", "20", "1 l", true, "")
	p.BeforePrice = ptr(money("25"))
	p.OfferUntil = ptr(mustDate(t, "2026-11-04"))
	s := &fake{catalog: []model.Product{p}, terms: termsN,
		info: model.DeliveryInfo{ChosenStart: chosenAware(time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC))}}
	q, err := compare.Quote(context.Background(), s, []string{"mælk 1 l"})
	if err != nil || len(q.Lines) != 1 || q.Lines[0].LineTotal.String() != "25.00" {
		t.Fatalf("%v %+v", err, q)
	}
	// the same time with no zone is the 4th, and the offer still counts
	s.info = model.DeliveryInfo{ChosenStart: chosenNaive(time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC))}
	q, _ = compare.Quote(context.Background(), s, []string{"mælk 1 l"})
	if q.Lines[0].LineTotal.String() != "20.00" {
		t.Errorf("naive: %s", q.Lines[0].LineTotal)
	}
}

func chosenAware(t time.Time) *model.DateTime { v := model.NewDateTime(t); return &v }
func chosenNaive(t time.Time) *model.DateTime { v := model.NaiveDateTime(t); return &v }

func mustDate(t *testing.T, s string) model.Date {
	d, err := model.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestQuoteErrorsFromTheStoreStopTheQuote(t *testing.T) {
	s := &fake{catalog: nemligCatalog, terms: termsN, infoErr: errors.New("network down")}
	if _, err := compare.Quote(context.Background(), s, []string{"mælk"}); err == nil {
		t.Error("an error that is neither NotImplemented nor a StoreError must not be swallowed")
	}
	s = &fake{catalog: nemligCatalog, terms: termsN, searchEr: store.Errorf("kø")}
	if _, err := compare.Quote(context.Background(), s, []string{"mælk"}); err == nil || err.Error() != "kø" {
		t.Errorf("search error: %v", err)
	}
}

func TestSearchSortKeepsInputOrderOnTies(t *testing.T) {
	mk := func(id string, unit string) model.Product {
		p := model.NewProduct("x", id, id, money("10"))
		if unit != "" {
			p.UnitPrice = ptr(money(unit))
		}
		return p
	}
	s := &limitStore{products: []model.Product{mk("a", ""), mk("b", "5"), mk("c", "5.00"), mk("d", ""), mk("e", "0")}}
	hits, err := compare.Search(context.Background(), s, "x", 10, "unit_price")
	if err != nil {
		t.Fatal(err)
	}
	var ids string
	for _, h := range hits {
		ids += h.ID
	}
	if ids != "ebcad" {
		t.Errorf("order %s, want ebcad", ids)
	}
	if _, err := compare.Search(context.Background(), s, "x", 10, "name"); err == nil {
		t.Error("an unknown sort is an error")
	}
}

func TestBestMatchPrefersWholeWordAndSize(t *testing.T) {
	kakao, other := nemligCatalog[1], prod("x", "m", "Mælk", "12.00", "1 l", true, "")
	line := compare.BestMatch("mælk 1 l", []model.Product{kakao, other}, nil)
	if line.Product == nil || line.Product.ID != "m" || !line.Sure {
		t.Errorf("%+v", line)
	}
	line = compare.BestMatch("toiletpapir", []model.Product{bilkaCatalog[14]}, nil) // sold out
	if line.Product != nil || line.Note == nil || *line.Note != "Ikke fundet" {
		t.Errorf("%+v", line)
	}
	kaffe := compare.BestMatch("kaffe 400 g", nemligCatalog, nil)
	if kaffe.Product == nil || kaffe.Product.ID != "n14" || kaffe.Sure || kaffe.Note == nil || *kaffe.Note != "Anden størrelse: 1 x 500 g." {
		t.Errorf("%+v", kaffe)
	}
}
