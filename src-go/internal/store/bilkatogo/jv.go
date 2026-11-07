package bilkatogo

// Readers for BilkaToGo's JSON. The shared ones live in package jv (also used by nemlig); the
// names here are thin aliases, so the code reads as the Python did. The guard carries this
// store's own message.

import "github.com/McAndersDK/handle-mcp-go/src-go/internal/store/jv"

type obj = jv.Obj

var (
	decodeJSON  = jv.DecodeJSON
	truthy      = jv.Truthy
	objOrEmpty  = jv.ObjOrEmpty
	listOrEmpty = jv.ListOrEmpty
	asObj       = jv.AsObj
	field       = jv.Field
	withDefault = jv.WithDefault
	has         = jv.Has
	pyStr       = jv.PyStr
	pyRepr      = jv.PyRepr
	toInt       = jv.ToInt
	toIntOrZero = jv.ToIntOrZero
	numVal      = jv.NumVal
	isNum       = jv.IsNum
	isPyInt     = jv.IsPyInt
	optStr      = jv.OptStr
	textOf      = jv.TextOf
	mustText    = jv.MustText
	strPtr      = jv.StrPtr
)

func bad(format string, args ...any) { jv.Bad(format, args...) }
func raise(err error)                { jv.Raise(err) }

const shapeMessage = "BilkaToGo svarede i et uventet format. API'et kan have ændret sig."

// guard turns a shape problem into a StoreError and lets a raised StoreError through as it is.
// Use it as "defer guard(&err)". Only the text of a shape problem is logged, never a value.
func guard(err *error) {
	if r := recover(); r != nil {
		*err = jv.Recover(r, shapeMessage, "bilkatogo")
	}
}
