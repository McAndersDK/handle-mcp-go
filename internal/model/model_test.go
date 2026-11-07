package model_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

func TestMoneyRoundsHalfUpOnlyWhenMarshalled(t *testing.T) {
	for in, want := range map[string]string{
		"0.125": "0.13", "12.995": "13.00", "39": "39.00", "-0.125": "-0.13",
		"0.1": "0.10", "12.949999": "12.95",
	} {
		b, err := json.Marshal(mustMoney(t, in))
		if err != nil || string(b) != `"`+want+`"` {
			t.Errorf("%s -> %s, %v; want %q", in, b, err, want)
		}
	}
}

func TestMoneyDoesNotRoundInArithmetic(t *testing.T) {
	a := mustMoney(t, "0.124")
	sum := a.Add(a) // 0.248 -> 0.25; rounding each first would give 0.24
	if sum.String() != "0.25" {
		t.Errorf("0.124 + 0.124 = %s", sum)
	}
	if got := mustMoney(t, "0.125").Mul(3).String(); got != "0.38" { // 0.375
		t.Errorf("0.125 * 3 = %s", got)
	}
	q, err := mustMoney(t, "1").Div(mustMoney(t, "3"))
	if err != nil || q.Mul(3).String() != "1.00" || q.Cmp(mustMoney(t, "0.33")) <= 0 {
		t.Errorf("1/3 = %s, %v", q, err)
	}
	if _, err := q.Div(model.Money{}); err == nil {
		t.Error("division by zero must fail")
	}
}

func TestMoneyZeroValueAndRawNumbers(t *testing.T) {
	if s := (model.Money{}).String(); s != "0.00" {
		t.Errorf("zero Money = %s", s)
	}
	var m model.Money
	if err := json.Unmarshal([]byte(`1234567890123456789.125`), &m); err != nil || m.String() != "1234567890123456789.13" {
		t.Errorf("a number beyond float64 precision gave %s, %v", m, err)
	}
	for _, bad := range []string{`"abc"`, `"NaN"`, `1e99999`, `""`} {
		if err := json.Unmarshal([]byte(bad), &m); err == nil {
			t.Errorf("%s must fail", bad)
		}
	}
}

func TestEmptySlicesAndMapsAreNotNull(t *testing.T) {
	b, err := json.Marshal(model.Cart{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"lines":[]`, `"warnings":[]`, `"fees":{}`, `"minimum_order":null`, `"minimum_met":null`} {
		if !strings.Contains(string(b), s) {
			t.Errorf("missing %s in %s", s, b)
		}
	}
	b, _ = json.Marshal(model.Order{})
	if !strings.Contains(string(b), `"lines":[]`) || !strings.Contains(string(b), `"placed_at":null`) {
		t.Errorf("order: %s", b)
	}
	b, _ = json.Marshal(model.RecipeSection{})
	if string(b) != `{"title":null,"steps":[]}` {
		t.Errorf("section: %s", b)
	}
}

func TestDefaultsFromConstructors(t *testing.T) {
	p := model.NewProduct("nemlig", "1", "Mælk", mustMoney(t, "12.95"))
	if !p.Available || p.OnOffer {
		t.Errorf("product defaults: %+v", p)
	}
	if s := model.NewStoreTerms("nemlig", true); s.Source != "api" {
		t.Errorf("terms source %q", s.Source)
	}
}

func TestDateTimeLikeIsoformat(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-03T16:00:00":               "2026-11-03T16:00:00",
		"2026-11-03 16:00:00":               "2026-11-03T16:00:00",
		"2026-11-03T16:00:00Z":              "2026-11-03T16:00:00+00:00",
		"2026-11-03T16:00:00+0100":          "2026-11-03T16:00:00+01:00",
		"2026-11-03T16:00:00.5-05:30":       "2026-11-03T16:00:00.500000-05:30",
		"2026-11-03T16:00:00.0000000":       "2026-11-03T16:00:00",
		"2026-11-03T16:00:00.1234567+01:00": "2026-11-03T16:00:00.123456+01:00", // 7-digit .NET, cut to 6
		"2026-11-03T16:00":                  "2026-11-03T16:00:00",
		"2026-11-03":                        "2026-11-03T00:00:00",
	} {
		v, err := model.ParseDateTime(in)
		if err != nil || v.String() != want {
			t.Errorf("%q -> %q, %v; want %q", in, v.String(), err, want)
		}
	}
	for _, bad := range []string{"", "03/11-2026 kl. 14-21", "2026-13-01T00:00:00", "2026-02-30", "2026-11-03T25:00:00",
		"2026-11-03T16:00:00+25:00", "2026-11-03T16:00:00.", "nonsense"} {
		if _, err := model.ParseDateTime(bad); err == nil {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestDateTimeKeepsWallClockAroundDST(t *testing.T) {
	cph := time.FixedZone("CET", 3600)
	v := model.NewDateTime(time.Date(2026, 3, 29, 2, 30, 0, 0, cph))
	if v.String() != "2026-03-29T02:30:00+01:00" || !v.Aware() {
		t.Errorf("got %s", v)
	}
	n := model.NaiveDateTime(time.Date(2026, 11, 2, 23, 0, 0, 0, cph))
	if b, _ := json.Marshal(n); string(b) != `"2026-11-02T23:00:00"` || n.Aware() {
		t.Errorf("naive: %s", b)
	}
}
