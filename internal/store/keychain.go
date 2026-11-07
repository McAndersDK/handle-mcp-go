package store

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// KeychainService is the service name; one entry per store, the secret is {"email", "session"}
// as JSON.
const KeychainService = "handle-mcp"

// KeychainError means there is no usable keychain on this machine (e.g. a headless Linux box).
// The text is only the Go type of the cause, never the cause's own text.
type KeychainError struct{ Kind string }

func (e *KeychainError) Error() string { return e.Kind }

func keychainErr(err error) error { return &KeychainError{Kind: fmt.Sprintf("%T", err)} }

// KeychainGet is the login saved by `handle-mcp login <store>` in the OS keychain, or nil.
// Any read error except "not found" means no keychain here: it is treated as none, so
// LoadLogin falls back to the file.
func KeychainGet(store string) (*Login, error) {
	secret, err := keyring.Get(KeychainService, store)
	if err != nil || secret == "" {
		return nil, nil
	}
	login := parse([]byte(secret))
	if login == nil {
		return nil, Errorf("Ugyldigt login i nøgleringen. Kør: handle-mcp login %s", store)
	}
	return login, nil
}

type saved struct {
	Email   string            `json:"email"`
	Session map[string]string `json:"session"`
}

// KeychainSet saves the store's session, never the password (ADR 0003).
func KeychainSet(store, email string, session map[string]string) error {
	secret, err := json.Marshal(saved{Email: email, Session: session})
	if err != nil {
		return keychainErr(err)
	}
	err = keyring.Set(KeychainService, store, string(secret))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrSetDataTooBig):
		return Errorf("sessionen er for stor til nøgleringen. Brug --from-env, så gemmes den i en fil")
	}
	return keychainErr(err)
}

// KeychainDelete is true if a login was removed, false if none was saved. A *KeychainError
// means there is no keychain here.
func KeychainDelete(store string) (bool, error) {
	err := keyring.Delete(KeychainService, store)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, keyring.ErrNotFound):
		return false, nil
	}
	return false, keychainErr(err)
}
