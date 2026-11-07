package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// loginAndChoose is the login of a store that needs choices saved too (store.Chooser). They are
// made here, in the terminal (a search, then numbers in the order of preference), or with
// HANDLE_<STORE>_STORE_ID, a comma-separated list of keys. Nothing is set at the store.
func (r runner) loginAndChoose(ctx context.Context, name string, st store.Store, ch store.Chooser) (map[string]string, error) {
	envName := "HANDLE_" + strings.ToUpper(name) + "_STORE_ID"
	var picked []store.Choice
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		_, choices, err := ch.Choices(ctx, "")
		if err != nil {
			return nil, err
		}
		if picked, err = pickByKeys(envName, raw, choices); err != nil {
			return nil, err
		}
	} else if !r.t.IsTerminal {
		return nil, store.Errorf("Kan ikke spørge her. Sæt %s til et eller flere nummer (kommasepareret, den foretrukne først), og kør igen. Kør login i en terminal for at se numrene.", envName)
	} else {
		var err error
		if picked, err = r.askAndPick(ctx, ch); err != nil {
			return nil, err
		}
	}
	session := st.Session()
	if len(session) == 0 {
		return nil, store.Errorf("%s gav ingen session tilbage. API'et kan have ændret sig.", name)
	}
	return ch.WithChoices(session, picked), nil
}

// pickByKeys takes the keys named by the env var, in its order. A key twice counts once.
func pickByKeys(envName, raw string, choices []store.Choice) ([]store.Choice, error) {
	var out []store.Choice
	seen := map[string]bool{}
	for _, key := range strings.Split(raw, ",") {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		found := false
		for _, c := range choices {
			if c.Key == key {
				out, found = append(out, c), true
				break
			}
		}
		if !found {
			return nil, store.Errorf("%s=%s er ikke på listen.", envName, key)
		}
	}
	if len(out) == 0 {
		return nil, store.Errorf("%s er tom.", envName)
	}
	return out, nil
}

// askAndPick asks for a search text, lists the matches on stderr and asks for numbers.
func (r runner) askAndPick(ctx context.Context, ch store.Chooser) ([]store.Choice, error) {
	query := ""
	if prompt := ch.SearchPrompt(); prompt != "" {
		fmt.Fprint(r.t.Stderr, prompt)
		if query = strings.TrimSpace(readLine(r.t.In)); query == "" {
			return nil, store.Errorf("Skriv noget at søge efter.")
		}
	}
	question, choices, err := ch.Choices(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(choices) == 0 {
		return nil, store.Errorf("Ingen passer til din søgning. Prøv igen med et andet søgeord.")
	}
	r.say(question)
	for i, c := range choices {
		r.say(fmt.Sprintf("  %d. %s", i+1, c.Label))
	}
	fmt.Fprintf(r.t.Stderr, "Vælg et eller flere numre (1-%d), den foretrukne først, f.eks. 2,1: ", len(choices))
	idx, ok := parseNumbers(readLine(r.t.In), len(choices))
	if !ok {
		return nil, store.Errorf("Ugyldigt valg.")
	}
	out := make([]store.Choice, len(idx))
	for i, n := range idx {
		out[i] = choices[n-1]
	}
	return out, nil
}

// parseNumbers reads "2,1" as [2 1]. Every number must be 1..max and appear once; nothing else
// is allowed, and an empty line is not a choice.
func parseNumbers(line string, max int) ([]int, bool) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(strings.TrimSpace(line), ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > max || seen[n] {
			return nil, false
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, len(out) > 0
}
