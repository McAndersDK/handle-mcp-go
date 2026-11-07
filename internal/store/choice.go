package store

import "context"

// Choice is one thing the human picks after a login, e.g. a pickup store.
type Choice struct {
	Key   string // saved with the session
	Label string // shown to the human, on their own terminal only
}

// Chooser is a store that needs choices saved with its login (REMA 1000: the pickup stores, in
// the order the human prefers them). `handle-mcp login` asks for them after the login worked. The
// choices are kept on this machine only: nothing is set at the store (docs/adr/0001).
type Chooser interface {
	// SearchPrompt is the Danish text that asks for a search text, shown before the list. It is
	// empty when the list needs no search.
	SearchPrompt() string
	// Choices logs in (the login is in ctx) and lists what matches the search text. An empty
	// text lists everything the store can. The question is the Danish text shown above the list.
	Choices(ctx context.Context, query string) (question string, choices []Choice, err error)
	// WithChoices returns the session to save, with the picked choices added, in the order the
	// human gave. It replaces any earlier choice in the session.
	WithChoices(session map[string]string, picked []Choice) map[string]string
}
