package pystr_test

import (
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
)

func TestReprEscapesWhatPythonEscapes(t *testing.T) {
	in := "\x07" + string(rune(0x200b)) // a bell and a zero width space
	want := `'` + `\x07` + `\u` + `200b` + `'`
	if got := pystr.Repr(in); got != want {
		t.Errorf("Repr = %s, want %s", got, want)
	}
}
