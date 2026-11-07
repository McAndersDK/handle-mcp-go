package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// loginAndChoose is the login of a store that needs one more choice (store.Chooser). The choice
// is made here, in the terminal, or with HANDLE_<STORE>_STORE_ID. Nothing is set at the store.
func (r runner) loginAndChoose(ctx context.Context, name string, st store.Store, ch store.Chooser) (map[string]string, error) {
	question, choices, err := ch.Choices(ctx)
	if err != nil {
		return nil, err
	}
	session := st.Session()
	if len(session) == 0 {
		return nil, store.Errorf("%s gav ingen session tilbage. API'et kan have ændret sig.", name)
	}
	picked, err := r.pick(name, question, choices)
	if err != nil {
		return nil, err
	}
	return ch.WithChoice(session, picked), nil
}

// pick takes the choice named by the env var, or asks for a number in the terminal.
func (r runner) pick(name, question string, choices []store.Choice) (store.Choice, error) {
	if len(choices) == 0 {
		return store.Choice{}, store.Errorf("Der er ingen at vælge mellem hos %s lige nu.", name)
	}
	envName := "HANDLE_" + strings.ToUpper(name) + "_STORE_ID"
	if key := strings.TrimSpace(os.Getenv(envName)); key != "" {
		for _, c := range choices {
			if c.Key == key {
				return c, nil
			}
		}
		return store.Choice{}, store.Errorf("%s=%s er ikke på listen.", envName, key)
	}
	if !r.t.IsTerminal {
		r.say(question)
		for _, c := range choices {
			r.say(fmt.Sprintf("  %s  %s", c.Key, c.Label))
		}
		return store.Choice{}, store.Errorf("Kan ikke spørge her. Sæt %s til et af numrene ovenfor, og kør igen.", envName)
	}
	r.say(question)
	for i, c := range choices {
		r.say(fmt.Sprintf("  %d. %s", i+1, c.Label))
	}
	fmt.Fprintf(r.t.Stderr, "Vælg et nummer (1-%d): ", len(choices))
	n, err := strconv.Atoi(strings.TrimSpace(readLine(r.t.In)))
	if err != nil || n < 1 || n > len(choices) {
		return store.Choice{}, store.Errorf("Ugyldigt valg.")
	}
	return choices[n-1], nil
}
