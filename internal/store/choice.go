package store

import "context"

// Choice is one thing the human picks after a login, e.g. a pickup store.
type Choice struct {
	Key   string // saved with the session
	Label string // shown to the human
}

// Chooser is a store that needs one choice saved with its login (REMA 1000: the pickup store).
// `handle-mcp login` asks for it after the login worked. The choice is kept on this machine
// only: nothing is set at the store (docs/adr/0001).
type Chooser interface {
	// Choices logs in (the login is in ctx) and lists what the human can pick from. The question
	// is the Danish text shown above the list.
	Choices(ctx context.Context) (question string, choices []Choice, err error)
	// WithChoice returns the session to save, with the choice added.
	WithChoice(session map[string]string, c Choice) map[string]string
}
