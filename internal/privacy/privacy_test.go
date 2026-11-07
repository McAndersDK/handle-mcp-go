package privacy_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

// leak is conftest.LEAK from the Python tests.
const leak = `{
 "CustomerName": "Anders And",
 "Email": "anders@example.com",
 "MobileNumber": "+45 98 76 54 32",
 "DoorCode": "kode-4711",
 "UnattendedNotes": "Nøglen ligger under måtten",
 "deliveryAddress": {"street": "Andeby 1", "zip": "postnr-9999", "city": "Andeby"}
}`

func TestOutLeavesNoLeak(t *testing.T) {
	var secrets map[string]any
	if err := json.Unmarshal([]byte(leak), &secrets); err != nil {
		t.Fatal(err)
	}
	answer := map[string]any{"name": "Mælk 1 l", "price": "12.95"}
	for k, v := range secrets {
		answer[k] = v
	}
	cart := map[string]any{"cart": answer, "list": []any{secrets}}
	text, err := privacy.Out(cart)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Anders And", "anders@example.com", "98 76 54 32", "kode-4711",
		"Nøglen", "Andeby", "postnr-9999"} {
		if strings.Contains(text, s) {
			t.Errorf("leaked %q in %s", s, text)
		}
	}
	if !strings.Contains(text, privacy.Redacted) || !strings.Contains(text, "Mælk 1 l") {
		t.Errorf("redaction marker or product name missing: %s", text)
	}
}

func TestOutWrapsNonObjects(t *testing.T) {
	for in, want := range map[string]string{
		`"x"`: "{\n  \"result\": \"x\"\n}",
		`5`:   "{\n  \"result\": 5\n}",
	} {
		var v any
		_ = json.Unmarshal([]byte(in), &v)
		got, err := privacy.Out(v)
		if err != nil || got != want {
			t.Errorf("Out(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	got, _ := privacy.Out([]any{map[string]any{"phone": "1"}})
	if want := "{\n  \"result\": [\n    {\n      \"phone\": \"" + privacy.Redacted + "\"\n    }\n  ]\n}"; got != want {
		t.Errorf("list wrap: %q", got)
	}
}

func TestOutKeepsNumbersAndHTML(t *testing.T) {
	in := map[string]any{"a": json.Number("1.50"), "b": json.Number("249.87"), "c": "Ben & Jerry's <3"}
	got, err := privacy.Out(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"a": 1.50`, `"b": 249.87`, `Ben & Jerry's <3`} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %s", s, got)
		}
	}
	if strings.Contains(got, `0026`) || strings.Contains(got, `003c`) {
		t.Errorf("HTML escaped: %s", got)
	}
}

func TestOutModelEmptySlicesAndNull(t *testing.T) {
	got, err := privacy.Out(model.Cart{Store: "nemlig"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"lines": []`, `"warnings": []`, `"fees": {}`, `"minimum_order": null`,
		`"total": "0.00"`} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %s", s, got)
		}
	}
}

func TestScrubKeepsProductNames(t *testing.T) {
	in := map[string]any{"name": "Kildevand 0,5 l 24 stk", "brand": "Kildevej Mejeri 12"}
	if out := privacy.Scrub(in); !reflect.DeepEqual(out, in) {
		t.Errorf("product fields changed: %v", out)
	}
	if out := privacy.ScrubText("Kildevand 0,5 l 24 stk"); out != "Kildevand 0,5 l 24 stk" {
		t.Errorf("text changed: %v", out)
	}
}

func TestDanishLettersAreWordCharacters(t *testing.T) {
	for in, want := range map[string]string{
		"Ærøvej 12":         privacy.Redacted,
		"Østergade 3B":      privacy.Redacted,
		"Søndre Allé 4":     "Søndre " + privacy.Redacted,
		"blåbær@mælk.dk":    privacy.Redacted,
		"2100 København Ø":  privacy.Redacted,
		"å1000 København":   "å1000 København", // Unicode \b: å is a word character
		"Kildevej 12Æ":      "Kildevej 12Æ",    // no \b between 2 and Æ
		"Kildevej 12b":      privacy.Redacted,
		"2100 København Øx": privacy.Redacted + " Øx", // " Ø" needs a b after it
	} {
		if got := privacy.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
}

// Definite-form street names are a leak Python has and Go fixes (ADR 0005).
func TestDefiniteStreetNames(t *testing.T) {
	for in, want := range map[string]string{
		"Åboulevarden 12":                privacy.Redacted,
		"Søndergaden 5":                  privacy.Redacted,
		"Rådhuspladsen 1":                privacy.Redacted,
		"Kongens Nytorvet 3":             "Kongens " + privacy.Redacted,
		"Birkealléen 7B":                 privacy.Redacted,
		"Vesterbrogaden 12":              privacy.Redacted,
		"Kildevand 0,5 l 24 stk":         "Kildevand 0,5 l 24 stk",
		"Levering fra torvet i morgen":   "Levering fra torvet i morgen",
		"Levering til Nytorvet i morgen": "Levering til Nytorvet i morgen",
	} {
		if got := privacy.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotesIsFreeText(t *testing.T) {
	got := privacy.Scrub(map[string]any{"notes": []any{"Ring 98765432"}})
	want := map[string]any{"notes": []any{"Ring " + privacy.Redacted}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
