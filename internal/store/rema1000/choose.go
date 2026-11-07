package rema1000

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// The second half of the gate: `handle-mcp login rema1000` lets the human search REMA's store
// list and pick one or more stores in order of preference. Whether a store has Køb & Hent does
// not matter here: REMA switches on by itself when one of them gets it (gate.go). The list is
// shown on the human's own terminal only.
const maxShown = 40

// SearchPrompt asks for a postcode or part of a name.
func (s *Store) SearchPrompt() string {
	return "Søg efter en REMA 1000-butik (postnummer eller en del af navn eller by): "
}

// Choices lists the stores that match the search text, read from the whole store list. The API
// has no search we know of, so the list is paged (up to 10 pages of 100) and filtered here. An
// empty text lists every store.
func (s *Store) Choices(ctx context.Context, query string) (string, []store.Choice, error) {
	all, err := s.listStores(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var choices []store.Choice
	for _, st := range all {
		if q != "" && !strings.Contains(strings.ToLower(st.Name+" "+st.PostalCode+" "+st.City), q) {
			continue
		}
		choices = append(choices, store.Choice{Key: strconv.FormatInt(st.ID, 10), Label: label(st)})
	}
	question := "Vælg de REMA 1000-butikker, du vil bruge. Søgningen sker lokalt i REMA 1000's butiksliste."
	if len(all) >= storePerPage*maxStorePages {
		question += " Kun de første " + strconv.Itoa(len(all)) + " butikker er søgt."
	}
	if q != "" && len(choices) > maxShown {
		question += fmt.Sprintf(" Der er %d træffere; kun de første %d vises. Søg mere præcist for at se flere.", len(choices), maxShown)
		choices = choices[:maxShown]
	}
	return question, choices, nil
}

// label is one line of the list: name, postcode and city, Køb & Hent yes or no, and the id.
func label(st apiStore) string {
	l := strings.TrimSpace(st.Name)
	if place := strings.TrimSpace(st.PostalCode + " " + st.City); place != "" {
		l += ", " + place
	}
	cc := "nej"
	if st.ClickAndCollect {
		cc = "ja"
	}
	return fmt.Sprintf("%s - Køb & Hent: %s (id %d)", l, cc, st.ID)
}

// WithChoices saves the picked store ids, in the human's order, as the session's list. It
// replaces an old single store_id. The list is kept on this machine.
func (s *Store) WithChoices(session map[string]string, picked []store.Choice) map[string]string {
	out := make(map[string]string, len(session)+1)
	for k, v := range session {
		out[k] = v
	}
	keys := make([]string, len(picked))
	for i, c := range picked {
		keys[i] = c.Key
	}
	delete(out, oldSavedKey)
	out[savedKey] = strings.Join(keys, ",")
	return out
}
