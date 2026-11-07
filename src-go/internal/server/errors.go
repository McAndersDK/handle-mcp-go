package server

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/compare"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

// toolError is one of our own tool errors (a bad argument, an unknown store, a limit). Its Danish
// text is shown as it is. Anything else that reaches a tool is a store failure or a bug.
type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }

func toolErrorf(format string, a ...any) error { return &toolError{fmt.Sprintf(format, a...)} }

// storeReason is the sentence for a failure in one store. A StoreError carries its own Danish
// text. For anything else only the type is shown and logged: the text of a Go error can hold a
// URL with ids or tokens.
func storeReason(name string, err error) string {
	var te *toolError
	var se *store.StoreError
	var pe *compare.PanicError
	switch {
	case errors.As(err, &te):
		return te.msg
	case errors.Is(err, store.ErrNotImplemented):
		return name + " kan ikke det endnu."
	case errors.As(err, &se):
		return name + ": " + se.Msg
	case errors.As(err, &pe):
		return unexpected(name, fmt.Sprintf("%T", pe.Value))
	}
	return unexpected(name, fmt.Sprintf("%T", err))
}

func unexpected(name, typ string) string {
	slog.Error("store failed", "store", name, "type", typ)
	return fmt.Sprintf("%s: uventet fejl (%s). Se serverens log.", name, typ)
}

// storeNames is the sorted list of known stores, for "Ukendt butik".
func (s *Server) storeNames() []string {
	names := make([]string, 0, len(s.factories))
	for n := range s.factories {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (s *Server) unknownStore(name string) error {
	known := strings.Join(s.storeNames(), ", ")
	if known == "" {
		known = "ingen endnu"
	}
	return toolErrorf("Ukendt butik '%s'. Kendte butikker: %s.", name, known)
}
