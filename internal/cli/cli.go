// Package cli is the command line for a human in a terminal: save or remove a store login.
//
//	handle-mcp login nemlig             asks for email and password, logs in, saves the store's
//	                                    session in the keychain
//	handle-mcp login nemlig --from-env  reads HANDLE_NEMLIG_EMAIL/_PASSWORD, logs in, saves the
//	                                    session in ~/.config/handle-mcp/nemlig.json (chmod 600).
//	                                    For a headless machine or an AI agent with no terminal.
//	handle-mcp logout nemlig            removes the login from the keychain and the file
//
// The password is used for one login and then forgotten: never saved, echoed or logged. Only the
// session the store gives back is saved (ADR 0003). When it expires, log in again.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"golang.org/x/term"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// Factory makes a fresh store. The login does not go in here: it comes through the context
// (store.WithLogin), so the store reads it with store.LoadLogin like any other call.
type Factory func() store.Store

// Terminal is what the commands talk to. Prompts and messages go to Stderr, never stdout.
type Terminal struct {
	In           io.Reader
	Stderr       io.Writer
	IsTerminal   bool                   // false: nobody can type a hidden password
	ReadPassword func() (string, error) // reads without echo
}

// DefaultTerminal is the process's own stdin and stderr.
func DefaultTerminal() Terminal {
	fd := int(os.Stdin.Fd())
	return Terminal{
		In:         os.Stdin,
		Stderr:     os.Stderr,
		IsTerminal: term.IsTerminal(fd),
		ReadPassword: func() (string, error) {
			b, err := term.ReadPassword(fd)
			return string(b), err
		},
	}
}

type runner struct {
	ctx    context.Context
	stores map[string]Factory
	t      Terminal
}

func (r runner) say(text string) { fmt.Fprintln(r.t.Stderr, text) }

// Run runs one command and returns the exit code: 0 ok, 1 failed, 2 wrong use.
func Run(ctx context.Context, args []string, stores map[string]Factory, t Terminal) int {
	r := runner{ctx: ctx, stores: stores, t: t}
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	slices.Sort(names)
	fromEnv := slices.Contains(args, "--from-env")
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--from-env" })
	if len(args) != 2 || (args[0] != "login" && args[0] != "logout") || stores[args[1]] == nil {
		r.say("Brug: handle-mcp login <butik> [--from-env]  eller  handle-mcp logout <butik>\n" +
			"Butikker: " + strings.Join(names, ", "))
		return 2
	}
	command, name := args[0], args[1]
	switch {
	case command == "logout":
		return r.logout(name)
	case fromEnv:
		return r.loginFromEnv(name)
	}
	return r.loginAsked(name)
}

const noTerminal = "Kodeordet kan ikke skrives skjult her. Kør kommandoen i PowerShell eller en anden " +
	"rigtig terminal, eller brug --from-env. Intet er gemt."

func (r runner) loginAsked(name string) int {
	if !r.t.IsTerminal || r.t.ReadPassword == nil {
		r.say(noTerminal)
		return 1
	}
	r.say("Log ind på " + name + ". Brug den email og det kodeord, du bruger på butikkens hjemmeside.")
	fmt.Fprintf(r.t.Stderr, "Email til %s: ", name)
	email := strings.TrimSpace(readLine(r.t.In))
	fmt.Fprintf(r.t.Stderr, "Kodeord til %s (vises ikke): ", name)
	password, err := r.t.ReadPassword()
	fmt.Fprintln(r.t.Stderr)
	if err != nil { // never fall back to reading with echo
		r.say(noTerminal)
		return 1
	}
	if email == "" || password == "" {
		r.say("Email og kodeord skal udfyldes. Intet er gemt.")
		return 1
	}
	// Log in first, save after: a failed login never touches a saved one.
	session, ok := r.check(name, email, password)
	if !ok {
		return 1
	}
	if err := store.KeychainSet(name, email, session); err != nil {
		r.say("Kunne ikke gemme i nøgleringen: " + err.Error())
		return 1
	}
	r.say("Logget ind på " + name + ". Sessionen er gemt i nøgleringen. Kodeordet er ikke gemt.")
	return 0
}

func (r runner) logout(name string) int {
	var removed []string
	gone, err := store.ConfigDelete(name)
	if err != nil {
		r.say(err.Error())
		return 1
	}
	if gone {
		removed = append(removed, "config-filen")
	}
	gone, err = store.KeychainDelete(name)
	switch {
	case err != nil && len(removed) == 0:
		r.say("Ingen nøglering på denne maskine: " + err.Error())
		return 1
	case gone:
		removed = append(removed, "nøgleringen")
	}
	if len(removed) == 0 {
		r.say("Der var intet gemt login.")
	} else {
		r.say("Login slettet fra " + strings.Join(removed, " og ") + ".")
	}
	return 0
}

// loginFromEnv is for no terminal and no keychain: a headless box or an AI agent. The env vars
// are read once, checked against the store and saved in a file only the owner can read.
func (r runner) loginFromEnv(name string) int {
	prefix := "HANDLE_" + strings.ToUpper(name) + "_"
	email, password := os.Getenv(prefix+"EMAIL"), os.Getenv(prefix+"PASSWORD")
	if email == "" || password == "" {
		r.say("Sæt " + prefix + "EMAIL og " + prefix + "PASSWORD, og kør igen. Intet er gemt.")
		return 1
	}
	session, ok := r.check(name, email, password)
	if !ok {
		return 1
	}
	path, err := store.ConfigSet(name, email, session)
	if err != nil {
		r.say("Login blev ikke gemt: " + err.Error())
		return 1
	}
	r.say("Logget ind på " + name + ". Sessionen er gemt i " + path + " (kun du kan læse den). " +
		"Kodeordet er ikke gemt: fjern " + prefix + "PASSWORD igen.")
	// A broken keychain entry counts as shadowing: LoadLogin will complain about it too.
	if saved, err := store.KeychainGet(name); saved != nil || err != nil { // LoadLogin reads it first
		r.say("OBS: nøgleringen har også et login til " + name + ", og det vinder over filen. " +
			"Fjern det med: handle-mcp logout " + name + ", og kør så --from-env igen.")
	}
	return 0
}

// check does a real, read-only login with these values and returns the session to save. The
// login goes to the store through the context, not the environment. It prints why on failure.
func (r runner) check(name, email, password string) (map[string]string, bool) {
	session, err := r.tryLogin(name, email, password)
	if err != nil {
		var se *store.StoreError
		text := "uventet fejl (" + fmt.Sprintf("%T", err) + ")." // the text of other errors may hold a URL
		if errors.As(err, &se) {
			text = se.Error()
		}
		r.say("Login blev ikke gemt: " + text + "\nEt tidligere gemt login er uændret.")
		return nil, false
	}
	return session, true
}

func (r runner) tryLogin(name, email, password string) (map[string]string, error) {
	ctx := store.WithLogin(r.ctx, name, store.Login{Email: email, Password: password})
	st := r.stores[name]()
	if ch, ok := st.(store.Chooser); ok { // a store that needs one more choice saved: choose.go
		return r.loginAndChoose(ctx, name, st, ch)
	}
	if _, err := st.GetCart(ctx); err != nil {
		return nil, err
	}
	session := st.Session()
	if len(session) == 0 {
		return nil, store.Errorf("%s gav ingen session tilbage. API'et kan have ændret sig.", name)
	}
	return session, nil
}

// readLine reads one line a byte at a time, so nothing after the newline is consumed.
func readLine(in io.Reader) string {
	var line []byte
	buf := make([]byte, 1)
	for len(line) < 4096 {
		if n, err := in.Read(buf); n == 1 {
			if buf[0] == '\n' {
				break
			}
			line = append(line, buf[0])
		} else if err != nil {
			break
		}
	}
	return string(line)
}
