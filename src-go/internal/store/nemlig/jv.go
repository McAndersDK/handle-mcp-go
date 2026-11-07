package nemlig

// Readers for nemlig's JSON. The shared ones live in package jv (also used by bilkatogo); the
// names here are thin aliases, so the code reads as it did. Money readers and the guard, which
// carries nemlig's own message, stay in this package.

import (
	"encoding/json"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store/jv"
)

type (
	obj        = jv.Obj
	shapeError = jv.ShapeError
)

var (
	decodeJSON  = jv.DecodeJSON
	truthy      = jv.Truthy
	objOrEmpty  = jv.ObjOrEmpty
	listOrEmpty = jv.ListOrEmpty
	asObj       = jv.AsObj
	field       = jv.Field
	withDefault = jv.WithDefault
	pyStr       = jv.PyStr
	pyRepr      = jv.PyRepr
	toInt       = jv.ToInt
	toIntOrZero = jv.ToIntOrZero
	toInt64     = jv.ToInt64
	optStr      = jv.OptStr
	textOf      = jv.TextOf
	strPtr      = jv.StrPtr
)

func bad(format string, args ...any) { jv.Bad(format, args...) }

const shapeMessage = "nemlig.com svarede i et uventet format. API'et kan have ændret sig."

// guard turns a shapeError into a StoreError. Use it as "defer guard(&err)". Only the text of the
// shape problem is logged, never a value from the answer.
func guard(err *error) {
	if r := recover(); r != nil {
		*err = jv.Recover(r, shapeMessage, "nemlig")
	}
}

// guardAs is guard with a message that names what changed.
func guardAs(err *error, msg string) {
	if r := recover(); r != nil {
		*err = jv.Recover(r, msg, "nemlig")
	}
}

// money is _money(): null stays missing, anything else must be a decimal number.
func money(v any) *model.Money {
	var text string
	switch x := v.(type) {
	case nil:
		return nil
	case json.Number:
		text = string(x)
	case string:
		text = x
	default:
		bad("not a number")
	}
	m, err := model.ParseMoney(text)
	if err != nil {
		bad("not a number")
	}
	return &m
}

// moneyOrZero is `_money(v) or Decimal(0)` for facts the model cannot leave empty.
func moneyOrZero(v any) model.Money {
	if m := money(v); m != nil {
		return *m
	}
	return model.Money{}
}

// requiredMoney is a price that must be there: a missing one is a format change, never 0 kr.
func requiredMoney(v any, what string) model.Money {
	m := money(v)
	if m == nil {
		bad("no %s", what)
	}
	return *m
}

// fromOre is Decimal(int(ore)) / 100.
func fromOre(v any) model.Money { return model.NewMoney(toInt64(v), -2) }

// rawStr is a value kept as it came: null stays null.
func rawStr(v any) *string {
	if v == nil {
		return nil
	}
	s := pyStr(v)
	return &s
}

// sliceTo is a[:n] for a slice, with Python's meaning of a negative n.
func sliceTo[T any](a []T, n int) []T {
	if n < 0 {
		n = max(len(a)+n, 0)
	}
	if n > len(a) {
		n = len(a)
	}
	return a[:n]
}
