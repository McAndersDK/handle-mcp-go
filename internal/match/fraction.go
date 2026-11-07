package match

import (
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

// Fractions are a Go-only addition (ADR 0005). Python's SIZE_RE reads "1/2 dl" as "2 dl", because
// the number after the slash wins. Here an amount is one of:
//
//	1,5   1.5   1/2   ½   1½   1 ½   1 1/2
//
// All of them are exact decimals. A fraction with denominator 0 is a bad match: it is consumed,
// so the "0 dl" behind the slash is not read as a size, and it gives no size either.

var vulgar = map[rune][2]int64{
	'½': {1, 2}, '⅓': {1, 3}, '⅔': {2, 3}, '¼': {1, 4}, '¾': {3, 4}, '⅕': {1, 5}, '⅖': {2, 5},
	'⅗': {3, 5}, '⅘': {4, 5}, '⅙': {1, 6}, '⅚': {5, 6}, '⅛': {1, 8}, '⅜': {3, 8}, '⅝': {5, 8}, '⅞': {7, 8},
}

func whole(rs []rune) (model.Money, bool) {
	m, err := model.ParseMoney(pystr.ASCIIDigits(string(rs)))
	return m, err == nil
}

func slashDigitsAt(rs []rune, n int) bool {
	return n+1 < len(rs) && rs[n] == '/' && pystr.IsDigit(rs[n+1])
}

// fraction reads "/den" at n (rs[n] is the slash) and divides num by it. bad is true for a
// denominator of 0 or one that is too large.
func fraction(rs []rune, n int, num model.Money) (val model.Money, end int, bad bool) {
	end = digitsEnd(rs, n+1)
	den, ok := whole(rs[n+1 : end])
	if !ok || den.Sign() == 0 {
		return model.Money{}, end, true
	}
	q, err := num.Div(den)
	return q, end, err != nil
}

// amountNumber reads the amount at i. end is where it stops; bad is true for a number that is
// no amount (denominator 0, or too large for a decimal).
func amountNumber(rs []rune, i int) (val model.Money, end int, bad, ok bool) {
	if i < len(rs) {
		if f, isFrac := vulgar[rs[i]]; isFrac {
			return ratio(f), i + 1, false, true
		}
	}
	j := digitsEnd(rs, i)
	if j == i {
		return model.Money{}, 0, false, false
	}
	w, wok := whole(rs[i:j])
	// "1½" and "1 ½"
	if k := spacesEnd(rs, j); k < len(rs) {
		if f, isFrac := vulgar[rs[k]]; isFrac {
			return w.Add(ratio(f)), k + 1, !wok, true
		}
	}
	// "1/2"
	if slashDigitsAt(rs, j) {
		v, e, bad := fraction(rs, j, w)
		return v, e, bad || !wok, true
	}
	// "1 1/2"
	if k := spacesEnd(rs, j); k > j {
		if n := digitsEnd(rs, k); n > k && slashDigitsAt(rs, n) {
			num, nok := whole(rs[k:n])
			v, e, bad := fraction(rs, n, num)
			return w.Add(v), e, bad || !wok || !nok, true
		}
	}
	// "1,5"
	if j+1 < len(rs) && (rs[j] == '.' || rs[j] == ',') && pystr.IsDigit(rs[j+1]) {
		j = digitsEnd(rs, j+1)
		d, err := model.ParseMoney(strings.ReplaceAll(pystr.ASCIIDigits(string(rs[i:j])), ",", "."))
		return d, j, err != nil, true
	}
	return w, j, !wok, true
}

func ratio(f [2]int64) model.Money {
	q, _ := model.NewMoney(f[0], 0).Div(model.NewMoney(f[1], 0))
	return q
}
