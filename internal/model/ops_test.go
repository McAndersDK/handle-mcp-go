package model_test

import (
	"math/big"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

func TestMoneyOps(t *testing.T) {
	a, b := mustMoney(t, "1.50"), mustMoney(t, "0.25")
	if got := a.Sub(b).String(); got != "1.25" {
		t.Errorf("Sub = %s", got)
	}
	if got := a.MulMoney(b).String(); got != "0.38" { // 0.375 rounds half up
		t.Errorf("MulMoney = %s", got)
	}
	if !mustMoney(t, "0.00").IsZero() || mustMoney(t, "0.01").IsZero() {
		t.Error("IsZero")
	}
	if mustMoney(t, "1.0").Cmp(mustMoney(t, "1.00")) != 0 {
		t.Error("Decimal comparison ignores trailing zeros")
	}
}

func TestCeilIsExact(t *testing.T) {
	for in, want := range map[string]string{"3": "3", "3.0000000000000000000000000001": "4", "2.5": "3", "0": "0",
		"-0.5": "0", "99999999999999999999.5": "100000000000000000000"} {
		if got := mustMoney(t, in).Ceil().String(); got != want {
			t.Errorf("Ceil(%s) = %s, want %s", in, got, want)
		}
	}
	if got := model.MoneyFromBig(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)).Ceil().String(); len(got) != 31 {
		t.Errorf("big ceil = %s", got)
	}
}

func TestFormatG(t *testing.T) {
	for in, want := range map[string]string{"6": "6", "2.50": "2.50", "1500.0": "1500.0", "0.5": "0.5"} {
		if got := mustMoney(t, in).FormatG(); got != want {
			t.Errorf("FormatG(%s) = %s", in, got)
		}
	}
	// the exponent survives a multiplication, as Decimal's does: 4.0 * 2 is 8.0
	if got := mustMoney(t, "4.0").MulMoney(model.NewMoney(2, 0)).FormatG(); got != "8.0" {
		t.Errorf("4.0 * 2 = %s", got)
	}
}

func TestRoundHalfUp(t *testing.T) {
	if got := mustMoney(t, "0.00995").Round(4).FormatG(); got != "0.0100" && got != "0.01" {
		t.Errorf("Round = %s", got)
	}
}

func TestDateCompareAndProductEqual(t *testing.T) {
	d1, _ := model.ParseDate("2026-11-02")
	d2, _ := model.ParseDate("2026-11-03")
	if d1.Compare(d2) != -1 || d2.Compare(d1) != 1 || d1.Compare(d1) != 0 {
		t.Error("Compare")
	}
	p := model.NewProduct("s", "1", "Mælk", mustMoney(t, "10.0"))
	q := model.NewProduct("s", "1", "Mælk", mustMoney(t, "10.00"))
	if !p.Equal(q) {
		t.Error("10.0 == 10.00")
	}
	q.OfferUntil = &d1
	if p.Equal(q) {
		t.Error("a date is a difference")
	}
}
