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
	for invalid := 1; ; invalid++ {
		fmt.Fprintf(r.t.Stderr, "Vælg et eller flere numre (1-%d), den foretrukne først, f.eks. 2,1 eller 1-2: ", len(choices))
		idx, ok := parseNumbers(readLine(r.t.In), len(choices))
		if ok {
			out := make([]store.Choice, len(idx))
			for i, n := range idx {
				out[i] = choices[n-1]
			}
			return out, nil
		}
		if invalid == maxChoiceTries {
			return nil, store.Errorf("Ugyldigt valg.")
		}
		r.say("Ugyldigt valg. Skriv numre som 2,1 eller 1-2.")
	}
}

// maxChoiceTries is how many invalid answers are allowed before the login gives up.
const maxChoiceTries = 3

// parseNumbers reads "2,1" as [2 1], "1-3" as [1 2 3] and "3-1" as [3 2 1]; ranges and numbers
// mix ("2,4-5"). Every number must be 1..max. A number that comes twice is dropped at its
// second place. Anything else, and an empty line, is not a choice.
func parseNumbers(line string, max int) ([]int, bool) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(line, ",") {
		from, to, isRange := strings.Cut(part, "-")
		lo, ok := parseNumber(from, max)
		if !ok {
			return nil, false
		}
		hi := lo
		if isRange {
			if hi, ok = parseNumber(to, max); !ok {
				return nil, false
			}
		}
		step := 1
		if hi < lo {
			step = -1
		}
		for n := lo; ; n += step {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
			if n == hi {
				break
			}
		}
	}
	return out, len(out) > 0
}

// parseNumber reads one number of a choice, spaces allowed, and checks it is 1..max.
func parseNumber(s string, max int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}
