package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Login is a store's login: an Email and either a Session or, for old entries and env vars, a
// Password. The Session is what a login gives back (ADR 0003); a Password is never saved.
type Login struct {
	Email    string
	Session  map[string]string
	Password string
}

// String, GoString and LogValue keep secrets out of logs and error text, whatever the verb.
func (l Login) String() string   { return "Login{redacted}" }
func (l Login) GoString() string { return "Login{redacted}" }
func (l Login) LogValue() slog.Value {
	return slog.StringValue("Login{redacted}")
}

type loginKey struct{ store string }

// WithLogin makes LoadLogin(ctx, store) return l before it looks at env vars, the keychain or
// the file, and makes SaveSession(ctx, store, ...) do nothing. `handle-mcp login` uses it to
// try a login without touching the process environment or a saved login.
func WithLogin(ctx context.Context, store string, l Login) context.Context {
	return context.WithValue(ctx, loginKey{store}, l)
}

func ctxLogin(ctx context.Context, store string) (Login, bool) {
	l, ok := ctx.Value(loginKey{store}).(Login)
	return l, ok
}

// parse reads a saved login: {"email", "session"}, or the old {"email", "password"} (still
// read, and replaced by a session on the next login: ADR 0003). nil when it is neither.
func parse(data []byte) *Login {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	var email string
	if string(raw["email"]) == "null" || json.Unmarshal(raw["email"], &email) != nil {
		// missing, null or not a string
		return nil
	}
	var session map[string]any
	if json.Unmarshal(raw["session"], &session) == nil && len(session) > 0 {
		out := make(map[string]string, len(session))
		for k, v := range session {
			s, ok := v.(string)
			if !ok {
				return nil // Python accepts any value; our sessions are strings only
			}
			out[k] = s
		}
		return &Login{Email: email, Session: out}
	}
	var password string
	if json.Unmarshal(raw["password"], &password) == nil && password != "" {
		return &Login{Email: email, Password: password}
	}
	return nil
}

func envPrefix(store string) string { return "HANDLE_" + strings.ToUpper(store) + "_" }

func envLogin(store string) (Login, bool) {
	email, password := os.Getenv(envPrefix(store)+"EMAIL"), os.Getenv(envPrefix(store)+"PASSWORD")
	if email == "" || password == "" {
		return Login{}, false
	}
	return Login{Email: email, Password: password}, true
}

// LoadLogin is a store's login. First found wins:
//
//  0. A login put in ctx with WithLogin.
//  1. Env vars HANDLE_<STORE>_EMAIL and HANDLE_<STORE>_PASSWORD.
//  2. The OS keychain, saved with `handle-mcp login <store>`. Recommended: encrypted at rest.
//  3. ~/.config/handle-mcp/<store>.json, readable by its owner only (chmod 600).
func LoadLogin(ctx context.Context, store string) (Login, error) {
	if l, ok := ctxLogin(ctx, store); ok {
		return l, nil
	}
	if l, ok := envLogin(store); ok {
		return l, nil
	}
	saved, err := KeychainGet(store)
	if err != nil {
		return Login{}, err
	}
	if saved != nil {
		return *saved, nil
	}

	path, err := configPath(store)
	if err != nil {
		return Login{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Login{}, Errorf("Intet login til %s. Kør i en terminal: handle-mcp login %s. "+
			"Uden terminal: sæt HANDLE_%s_EMAIL og _PASSWORD, "+
			"og kør: handle-mcp login %s --from-env", store, store, strings.ToUpper(store), store)
	}
	// Windows has no mode bits; the user profile's ACL keeps the file private (ADR 0004).
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return Login{}, Errorf("%s kan læses af andre. Kør: chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	var login *Login
	if err == nil {
		login = parse(data)
	}
	if login == nil {
		return Login{}, Errorf("%s er ugyldig. Kør: handle-mcp login %s --from-env", path, store)
	}
	return *login, nil
}

// HasLogin is true when LoadLogin(ctx, store) would succeed. For calls that work without one.
func HasLogin(ctx context.Context, store string) bool {
	_, err := LoadLogin(ctx, store)
	return err == nil
}

// SaveSession runs after a login with a password: it replaces a saved password with the
// session, where LoadLogin found it. Env vars and a ctx login are the human's own: nothing is
// written then. A failed save only logs a warning (the login works) with the error's type.
func SaveSession(ctx context.Context, store, email string, session map[string]string) {
	if _, ok := ctxLogin(ctx, store); ok {
		return
	}
	if _, ok := envLogin(store); ok {
		return
	}
	err := saveWhereFound(store, email, session)
	if err != nil {
		slog.Warn("could not save the session", "store", store, "error", fmt.Sprintf("%T", err))
	}
}

func saveWhereFound(store, email string, session map[string]string) error {
	saved, err := KeychainGet(store)
	if err != nil {
		return err
	}
	if saved != nil {
		return KeychainSet(store, email, session)
	}
	path, err := configPath(store)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		_, err = ConfigSet(store, email, session)
		return err
	}
	return nil
}

// Expired is the error for a saved session the store no longer accepts.
func Expired(store string) error {
	return Errorf("Login til %s er udløbet. Kør i en terminal: handle-mcp login %s", store, store)
}

func configPath(store string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, store+".json"), nil
}
