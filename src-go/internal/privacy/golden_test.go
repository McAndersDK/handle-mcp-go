package privacy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const goldenDir = "../../../tests/golden/privacy"

// Expected case counts. A golden that shrinks, or a case the test does not read, fails.
const (
	wantScrubCases    = 15
	wantTextStrings   = 68
	wantTextStructs   = 2
	wantKeyGroups     = 64
	wantKeySpellings  = 320
	wantKeyCases      = wantKeySpellings * 6
	wantPersonalKeys  = 56
	wantFreeTextKeys  = 5
	notesCasesPromise = 1 // scrub.json cases that differ on purpose (see below)
)

func readGolden(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestGoldenScrub(t *testing.T) {
	var g struct {
		Cases []struct {
			Name           string `json:"name"`
			Input          any    `json:"input"`
			Output         any    `json:"output"`
			InputUnchanged bool   `json:"input_unchanged"`
		} `json:"cases"`
	}
	readGolden(t, "scrub.json", &g)
	if len(g.Cases) != wantScrubCases {
		t.Fatalf("scrub.json has %d cases, want %d", len(g.Cases), wantScrubCases)
	}
	exceptions := 0
	for _, c := range g.Cases {
		want := c.Output
		// NAMED EXCEPTION (ADR 0005): Go scrubs the "notes" key as free text, Python does not.
		if c.Name == "notes_key_python_does_not_scrub" {
			want = map[string]any{"note": "Ring " + Redacted, "notes": "Ring " + Redacted}
			exceptions++
		}
		var before []byte
		before, _ = json.Marshal(c.Input)
		got := Scrub(c.Input)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want %#v", c.Name, got, want)
		}
		after, _ := json.Marshal(c.Input)
		if c.InputUnchanged && !bytes.Equal(before, after) {
			t.Errorf("%s: input was changed", c.Name)
		}
	}
	if exceptions != notesCasesPromise {
		t.Fatalf("notes exception touched %d cases, want %d", exceptions, notesCasesPromise)
	}
	t.Logf("scrub.json: %d cases run, %d named exception", len(g.Cases), exceptions)
}

func TestGoldenScrubText(t *testing.T) {
	var g struct {
		Redacted string `json:"redacted"`
		Strings  []struct{ Input, Output string }
		Structs  []struct{ Input, Output any } `json:"structures"`
	}
	readGolden(t, "scrub_text.json", &g)
	if g.Redacted != Redacted {
		t.Fatalf("marker %q, golden %q", Redacted, g.Redacted)
	}
	if len(g.Strings) != wantTextStrings || len(g.Structs) != wantTextStructs {
		t.Fatalf("scrub_text.json has %d strings and %d structures, want %d and %d",
			len(g.Strings), len(g.Structs), wantTextStrings, wantTextStructs)
	}
	for _, c := range g.Strings {
		if got := ScrubText(c.Input); got != c.Output {
			t.Errorf("ScrubText(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
	for i, c := range g.Structs {
		if got := ScrubText(c.Input); !reflect.DeepEqual(got, c.Output) {
			t.Errorf("structure %d: got %#v, want %#v", i, got, c.Output)
		}
	}
	t.Logf("scrub_text.json: %d strings, %d structures run", len(g.Strings), len(g.Structs))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestGoldenKeys(t *testing.T) {
	var g struct {
		FreeText []string                               `json:"free_text_keys"`
		Personal []string                               `json:"personal_keys"`
		Redacted string                                 `json:"redacted"`
		ByKey    map[string]map[string][]map[string]any `json:"scrub_by_key"`
		Values   []any                                  `json:"values"`
	}
	readGolden(t, "keys.json", &g)
	if len(g.Personal) != wantPersonalKeys || len(g.FreeText) != wantFreeTextKeys {
		t.Fatalf("keys.json has %d personal and %d free-text keys, want %d and %d",
			len(g.Personal), len(g.FreeText), wantPersonalKeys, wantFreeTextKeys)
	}
	if !reflect.DeepEqual(g.Personal, sortedKeys(personalKeys)) {
		t.Errorf("personal keys differ from the golden")
	}
	// NAMED EXCEPTION (ADR 0005): Go has "notes" as an extra free-text key.
	wantFree := append(append([]string{}, g.FreeText...), "notes")
	sort.Strings(wantFree)
	if !reflect.DeepEqual(wantFree, sortedKeys(freeTextKeys)) {
		t.Errorf("free-text keys are %v, want golden plus notes", sortedKeys(freeTextKeys))
	}
	if len(g.ByKey) != wantKeyGroups || len(g.Values) != 6 {
		t.Fatalf("keys.json has %d groups and %d values, want %d and 6", len(g.ByKey), len(g.Values), wantKeyGroups)
	}
	spellings, cases := 0, 0
	for group, bySpelling := range g.ByKey {
		for spelling, outs := range bySpelling {
			spellings++
			if len(outs) != len(g.Values) {
				t.Fatalf("%s/%s: %d outputs for %d values", group, spelling, len(outs), len(g.Values))
			}
			for i, want := range outs {
				cases++
				got := Scrub(map[string]any{spelling: g.Values[i]})
				// "notes" is free text in Go. None of the six values holds personal text,
				// so its expected output is the golden's. Checked, not assumed.
				if !reflect.DeepEqual(got, mapAny(want)) {
					t.Errorf("%s/%s[%d]: got %#v, want %#v", group, spelling, i, got, want)
				}
			}
		}
	}
	if spellings != wantKeySpellings || cases != wantKeyCases {
		t.Fatalf("ran %d spellings and %d cases, want %d and %d", spellings, cases, wantKeySpellings, wantKeyCases)
	}
	t.Logf("keys.json: %d groups, %d spellings, %d cases run, 0 differ for notes", len(g.ByKey), spellings, cases)
}

func mapAny(m map[string]any) any { return m }
