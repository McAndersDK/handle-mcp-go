package pricing_test

import (
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pricing"
)

func m(t *testing.T, s string) model.Money {
	t.Helper()
	v, err := model.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func ptr[T any](v T) *T { return &v }

func prod(t *testing.T, price string) *model.Product {
	p := model.NewProduct("t", "1", "Vare", m(t, price))
	return &p
}

func day(t *testing.T, s string) *model.Date {
	d, err := model.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestCostMultiBuy(t *testing.T) {
	p := prod(t, "22")
	p.OfferText = ptr("3 for 48 kr")
	for count, want := range map[int64]string{1: "22.00", 2: "44.00", 3: "48.00", 4: "70.00", 7: "118.00"} {
		if got := pricing.Cost(p, match.QtyOf(count), nil).String(); got != want {
			t.Errorf("Cost(%d) = %s, want %s", count, got, want)
		}
	}
	hint := pricing.MultiHint(p, match.QtyOf(2), nil)
	if hint == nil || *hint != "Tilbud 3 for 48 kr: 1 mere for 4.00 kr." {
		t.Errorf("MultiHint = %v", hint)
	}
	if pricing.MultiHint(p, match.QtyOf(3), nil) != nil {
		t.Error("no hint when the count is a whole offer")
	}
	// the offer is gone after its last day, with or without a before-price
	p.OfferUntil = day(t, "2026-11-02")
	if got := pricing.Cost(p, match.QtyOf(3), day(t, "2026-11-02")).String(); got != "48.00" {
		t.Errorf("on the last day the offer counts: %s", got)
	}
	if got := pricing.Cost(p, match.QtyOf(3), day(t, "2026-11-03")).String(); got != "66.00" {
		t.Errorf("after the last day the multi-buy is gone: %s", got)
	}
	// Unicode digits are \d in Python
	p.OfferUntil = nil
	p.OfferText = ptr("٣ for ٤٨ kr")
	if got := pricing.Cost(p, match.QtyOf(3), nil).String(); got != "48.00" {
		t.Errorf("Arabic-Indic digits: %s", got)
	}
	// "for" with a capital, and a decimal comma
	p.OfferText = ptr("3 FOR 47,50")
	if got := pricing.Cost(p, match.QtyOf(3), nil).String(); got != "47.50" {
		t.Errorf("3 FOR 47,50: %s", got)
	}
}

func TestZeroIsNotMissing(t *testing.T) {
	// before_price 0 is falsy in Python: the offer end does not switch to 0 kr
	p := prod(t, "10")
	p.BeforePrice = ptr(m(t, "0"))
	p.OfferUntil = day(t, "2026-01-01")
	if got := pricing.Cost(p, match.QtyOf(2), day(t, "2030-01-01")).String(); got != "20.00" {
		t.Errorf("zero before price: %s", got)
	}
	// max_per_order 0 is no limit
	p.MaxPerOrder = ptr(0)
	if got := pricing.Cost(p, match.QtyOf(2), nil).String(); got != "20.00" {
		t.Errorf("max_per_order 0: %s", got)
	}
	// a free gift, max 1: the second pack costs the normal price
	gift := prod(t, "0")
	gift.BeforePrice = ptr(m(t, "25"))
	gift.MaxPerOrder = ptr(1)
	if got := pricing.Cost(gift, match.QtyOf(3), nil).String(); got != "50.00" {
		t.Errorf("gift: %s", got)
	}
	// the limit caps a multi-buy too
	multi := prod(t, "22")
	multi.OfferText = ptr("3 for 48 kr")
	multi.MaxPerOrder = ptr(3)
	if got := pricing.Cost(multi, match.QtyOf(4), nil).String(); got != "70.00" {
		t.Errorf("limit and multi-buy: %s", got)
	}
}

func TestHugeCounts(t *testing.T) {
	q, _ := match.ParseQty("99999999999999999999")
	if got := pricing.Cost(prod(t, "119"), q, nil).String(); got != "11899999999999999999881.00" {
		t.Errorf("huge count: %s", got)
	}
}

func TestPacks(t *testing.T) {
	size := func(s string) *match.Size { return match.ParseSize(s) }
	withSize := func(s string) *model.Product { p := prod(t, "10"); p.Size = ptr(s); return p }
	cases := []struct {
		wanted string
		qty    int64
		have   string
		want   int64 // 0: none
	}{
		{"1,5 kg", 1, "500 g", 3},
		{"1 kg", 1, "990 g", 1}, // never less than wanted, 2 % short is fine
		{"1 kg", 1, "900 g", 2},
		{"400 g", 1, "500 g", 1},
		{"2 stk", 1, "1 kg", 0}, // kg against stk
		{"1 kg", 1, "", 0},
		{"8 stk", 1, "8 ruller", 1}, // rolls take stk
		{"8 ruller", 1, "42 stk", 0},
		{"1 l", 2, "1 l", 2},
		{"1/2 dl", 1, "2 dl", 1}, // a fraction is an amount: 50 ml, one pack
		{"½ l", 2, "2 dl", 5},     // 1000 ml from 200 ml packs
	}
	for _, c := range cases {
		p := prod(t, "10")
		if c.have != "" {
			p = withSize(c.have)
		}
		got, ok := pricing.Packs(size(c.wanted), match.QtyOf(c.qty), p)
		if (c.want == 0) == ok || (ok && got.String() != match.QtyOf(c.want).String()) {
			t.Errorf("Packs(%s x%d, %s) = %s, %v, want %d", c.wanted, c.qty, c.have, got, ok, c.want)
		}
	}
	// 6 bananas from 4-packs: 2 packs
	p := withSize("4 stk")
	if n, ok := pricing.Pieces(nil, match.QtyOf(6), p); !ok || n.String() != "2" {
		t.Errorf("Pieces = %s, %v", n, ok)
	}
	if _, ok := pricing.Pieces(nil, match.QtyOf(1), p); ok {
		t.Error("Pieces: one thing is no list of pieces")
	}
	if _, ok := pricing.Pieces(nil, match.QtyOf(6), withSize("1 stk")); ok {
		t.Error("Pieces: a pack of one")
	}
	if !pricing.TooMuch(size("400 g"), match.QtyOf(1), match.QtyOf(1), withSize("500 g")) {
		t.Error("TooMuch: 500 g for 400 g is 25 % more")
	}
	if pricing.TooMuch(size("400 g"), match.QtyOf(1), match.QtyOf(1), withSize("450 g")) {
		t.Error("TooMuch: 450 g for 400 g is 12.5 % more")
	}
}

func TestPerUnit(t *testing.T) {
	p := prod(t, "30")
	p.Size = ptr("500 g")
	u := pricing.PerUnit(p, match.QtyOf(1), match.QtyOf(1), nil)
	if u.Kind != 0 || u.Value.Cmp(m(t, "0.06")) != 0 {
		t.Errorf("per gram: %d %s", u.Kind, u.Value.FormatG())
	}
	if u := pricing.PerUnit(p, match.QtyOf(2), match.QtyOf(2), nil); u.Kind != 1 {
		t.Errorf("weight comes after pieces for a quantity over 1: %d", u.Kind)
	}
	none := prod(t, "30")
	if u := pricing.PerUnit(none, match.QtyOf(1), match.QtyOf(3), nil); u.Kind != 2 || u.Value.Cmp(m(t, "30")) != 0 {
		t.Errorf("no size: %d %s", u.Kind, u.Value.FormatG())
	}
}
