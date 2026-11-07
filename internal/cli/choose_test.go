package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// choosingStore is a store that needs choices saved (store.Chooser), like REMA 1000.
type choosingStore struct {
	store.Store
	listErr error
}

var allPicks = []store.Choice{{Key: "11", Label: "Butik A"}, {Key: "22", Label: "Butik B"}, {Key: "33", Label: "Anden C"}}

func (choosingStore) Name() string         { return "pick" }
func (choosingStore) SearchPrompt() string { return "Søg: " }
func (c choosingStore) Choices(ctx context.Context, query string) (string, []store.Choice, error) {
	l, err := store.LoadLogin(ctx, "pick")
	if err != nil || l.Password != password {
		return "", nil, store.Errorf("Tjek email og kodeord.")
	}
	if c.listErr != nil {
		return "", nil, c.listErr
	}
	var out []store.Choice
	for _, p := range allPicks {
		if strings.Contains(strings.ToLower(p.Label), strings.ToLower(query)) {
			out = append(out, p)
		}
	}
	return "Vælg butik:", out, nil
}
func (choosingStore) Session() map[string]string { return map[string]string{"token": token} }
func (choosingStore) WithChoices(s map[string]string, picked []store.Choice) map[string]string {
	keys := make([]string, len(picked))
	for i, c := range picked {
		keys[i] = c.Key
	}
	out := map[string]string{"store_ids": strings.Join(keys, ",")}
	for k, v := range s {
		out[k] = v
	}
	return out
}

func runPick(t *testing.T, terminal bool, typed string, args ...string) (int, string) {
	t.Helper()
	var errBuf bytes.Buffer
	code := Run(bg, args, map[string]Factory{"pick": func() store.Store { return choosingStore{} }}, Terminal{
		In:           strings.NewReader("a@example.com\n" + typed),
		Stderr:       &errBuf,
		IsTerminal:   terminal,
		ReadPassword: func() (string, error) { return password, nil },
	})
	return code, errBuf.String()
}

func pickEnv(t *testing.T) {
	t.Helper()
	resetWorld(t)
	t.Setenv("HANDLE_CONFIG_DIR", t.TempDir())
	t.Setenv("HANDLE_PICK_STORE_ID", "")
	os.Unsetenv("HANDLE_PICK_STORE_ID")
}

func resetWorld(t *testing.T) { t.Helper(); env(t) }

func savedIDs(t *testing.T) string {
	t.Helper()
	l, err := store.KeychainGet("pick")
	if err != nil || l == nil {
		return ""
	}
	return l.Session["store_ids"]
}

func TestLoginSearchesAndSavesSeveralChoicesInOrder(t *testing.T) {
	pickEnv(t)
	code, out := runPick(t, true, "butik\n2,1\n", "login", "pick")
	l, err := store.KeychainGet("pick")
	if code != 0 || err != nil || l == nil || l.Session["store_ids"] != "22,11" || l.Session["token"] != token || l.Password != "" {
		t.Fatalf("code=%d login=%v err=%v\n%s", code, l, err, out)
	}
	if !strings.Contains(out, "1. Butik A") || !strings.Contains(out, "2. Butik B") || strings.Contains(out, "Anden C") {
		t.Errorf("the search result was not shown:\n%s", out)
	}
	if strings.Contains(out, password) {
		t.Error("the password is in the output")
	}
}

func TestLoginWithOneChoice(t *testing.T) {
	pickEnv(t)
	if code, out := runPick(t, true, "anden\n1\n", "login", "pick"); code != 0 || savedIDs(t) != "33" {
		t.Fatalf("code=%d ids=%q\n%s", code, savedIDs(t), out)
	}
}

func TestBadChoiceInputSavesNothing(t *testing.T) {
	// The search lists 2 butiks, so 3 is out of range.
	for _, typed := range []string{"0", "3", "3,3", "x", "1,x", "", " ", ",", "1,,2", "-1", "a-2", "1-9", "2-", "1-2-3"} {
		pickEnv(t)
		code, out := runPick(t, true, "butik\n"+strings.Repeat(typed+"\n", 3), "login", "pick")
		if code != 1 || savedIDs(t) != "" || !strings.Contains(out, "Ugyldigt valg") {
			t.Errorf("input %q: code=%d saved=%q\n%s", typed, code, savedIDs(t), out)
		}
	}
}

func TestRangesSaveInOrder(t *testing.T) {
	for typed, want := range map[string]string{"1-2\n": "11,22", "2-1\n": "22,11", " 1 - 2 \n": "11,22", "1,1-2\n": "11,22"} {
		pickEnv(t)
		code, out := runPick(t, true, "butik\n"+typed, "login", "pick")
		if code != 0 || savedIDs(t) != want {
			t.Errorf("input %q: code=%d saved=%q want %q\n%s", typed, code, savedIDs(t), want, out)
		}
	}
	pickEnv(t)
	if code, out := runPick(t, true, "butik\n1-2\n", "login", "pick"); code != 0 || !strings.Contains(out, "f.eks. 2,1 eller 1-2") {
		t.Fatalf("code=%d, the question does not mention ranges:\n%s", code, out)
	}
}

func TestInvalidThenValidAsksAgain(t *testing.T) {
	pickEnv(t)
	code, out := runPick(t, true, "butik\nx\n2,1\n", "login", "pick")
	if code != 0 || savedIDs(t) != "22,11" || strings.Count(out, "Ugyldigt valg. Skriv numre som 2,1 eller 1-2.") != 1 {
		t.Fatalf("code=%d saved=%q\n%s", code, savedIDs(t), out)
	}
}

func TestThreeInvalidAnswersSaveNothing(t *testing.T) {
	pickEnv(t)
	// The fourth answer is valid, but the login must have given up by then.
	code, out := runPick(t, true, "butik\n0\n3\nx\n1\n", "login", "pick")
	if code != 1 || savedIDs(t) != "" || !strings.Contains(out, "Ugyldigt valg.") {
		t.Fatalf("code=%d saved=%q\n%s", code, savedIDs(t), out)
	}
}

func TestParseNumbers(t *testing.T) {
	ok := map[string][]int{
		"2,1":     {2, 1},
		"1-2":     {1, 2},
		"3-1":     {3, 2, 1},
		"2,4-5":   {2, 4, 5},
		" 1 - 2 ": {1, 2},
		"2,1-2":   {2, 1},
		"4-4":     {4},
		"5-4,4,5": {5, 4},
		"1, 2 ,3": {1, 2, 3},
		"1-2,2-1": {1, 2},
	}
	for line, want := range ok {
		got, valid := parseNumbers(line, 5)
		if !valid || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: got %v valid=%v, want %v", line, got, valid, want)
		}
	}
	for _, line := range []string{"", " ", ",", "1,,2", "0", "6", "1-9", "a-2", "-1", "1-", "2-", "1-2-3", "x", "1,x", "1--2", "+"} {
		if got, valid := parseNumbers(line, 5); valid {
			t.Errorf("%q: accepted as %v", line, got)
		}
	}
}

func TestEmptyOrUnmatchedSearchSavesNothing(t *testing.T) {
	pickEnv(t)
	if code, out := runPick(t, true, "\n", "login", "pick"); code != 1 || savedIDs(t) != "" || !strings.Contains(out, "Skriv noget") {
		t.Fatalf("empty search: code=%d\n%s", code, out)
	}
	if code, out := runPick(t, true, "findesikke\n1\n", "login", "pick"); code != 1 || savedIDs(t) != "" || !strings.Contains(out, "Ingen passer") {
		t.Fatalf("no match: code=%d\n%s", code, out)
	}
}

func TestStoreIdEnvChoosesWithoutAsking(t *testing.T) {
	pickEnv(t)
	t.Setenv("HANDLE_PICK_STORE_ID", "33, 11,33")
	code, out := runPick(t, true, "", "login", "pick")
	if code != 0 || savedIDs(t) != "33,11" {
		t.Fatalf("code=%d ids=%q\n%s", code, savedIDs(t), out)
	}
	t.Setenv("HANDLE_PICK_STORE_ID", "11,99")
	if code, out := runPick(t, true, "", "login", "pick"); code != 1 || !strings.Contains(out, "er ikke på listen") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

func TestFromEnvWithoutATerminalNeedsTheStoreId(t *testing.T) {
	pickEnv(t)
	t.Setenv("HANDLE_PICK_EMAIL", "a@example.com")
	t.Setenv("HANDLE_PICK_PASSWORD", password)
	code, out := runPick(t, false, "", "login", "pick", "--from-env")
	if code != 1 || !strings.Contains(out, "HANDLE_PICK_STORE_ID") || savedIDs(t) != "" {
		t.Fatalf("code=%d\n%s", code, out)
	}
	t.Setenv("HANDLE_PICK_STORE_ID", "22,11")
	code, out = runPick(t, false, "", "login", "pick", "--from-env")
	if code != 0 {
		t.Fatalf("code=%d\n%s", code, out)
	}
	dir, _ := store.ConfigDir()
	data, err := os.ReadFile(dir + string(os.PathSeparator) + "pick.json")
	if err != nil || !strings.Contains(string(data), `"store_ids":"22,11"`) || strings.Contains(string(data), password) {
		t.Fatalf("file: %s err %v", data, err)
	}
}
