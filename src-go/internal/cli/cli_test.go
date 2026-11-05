package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

func TestMain(m *testing.M) {
	keyring.MockInit() // no test may reach the real keychain
	m.Run()
}

var bg = context.Background()

const (
	password = "rigtig-SECRET-pw"
	token    = "session-1-SECRET-tok"
)

// checkingStore logs in through store.LoadLogin, as the real adapters will. The CLI gives it
// the login in the context; this proves that path works with no adapter change.
type checkingStore struct {
	store.Store // nil: only the methods below are called
	failWith    error
}

func (checkingStore) Name() string { return "fake" }
func (c checkingStore) GetCart(ctx context.Context) (model.Cart, error) {
	if c.failWith != nil {
		return model.Cart{}, c.failWith
	}
	l, err := store.LoadLogin(ctx, "fake")
	if err != nil {
		return model.Cart{}, err
	}
	if l.Password != password {
		return model.Cart{}, store.Errorf("Tjek email og kodeord.")
	}
	return model.Cart{}, nil
}
func (checkingStore) Session() map[string]string { return map[string]string{"token": token} }

func factories(fail error) map[string]Factory {
	return map[string]Factory{"fake": func() store.Store { return checkingStore{failWith: fail} }}
}

// env gives a fresh keychain, a temp config dir and really unset HANDLE_FAKE_* env vars.
func env(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := filepath.Join(t.TempDir(), "handle-mcp")
	t.Setenv("HANDLE_CONFIG_DIR", dir)
	for _, k := range []string{"HANDLE_FAKE_EMAIL", "HANDLE_FAKE_PASSWORD"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	return dir
}

// run runs a command with typed-in email and password; stderr comes back as text.
func run(t *testing.T, fail error, typedEmail, typedPassword string, args ...string) (int, string) {
	t.Helper()
	var errBuf bytes.Buffer
	code := Run(bg, args, factories(fail), Terminal{
		In:           strings.NewReader(typedEmail + "\n"),
		Stderr:       &errBuf,
		IsTerminal:   true,
		ReadPassword: func() (string, error) { return typedPassword, nil },
	})
	return code, errBuf.String()
}

func saved(t *testing.T) *store.Login {
	t.Helper()
	l, err := store.KeychainGet("fake")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func fromEnv(t *testing.T, pw string) {
	t.Setenv("HANDLE_FAKE_EMAIL", "a@example.com")
	t.Setenv("HANDLE_FAKE_PASSWORD", pw)
}

func TestLoginSavesAWorkingLogin(t *testing.T) { // test_login_saves_a_working_login
	env(t)
	code, out := run(t, nil, "a@example.com", password, "login", "fake")
	l := saved(t)
	if code != 0 || l == nil || l.Email != "a@example.com" || l.Session["token"] != token || l.Password != "" {
		t.Fatalf("code=%d login=%v", code, l != nil)
	}
	if !strings.Contains(out, "Logget ind") {
		t.Fatalf("stderr: %s", out)
	}
	// The CLI has no stdout at all (Terminal has none), so "nothing on stdout" holds by shape.
	assertNoSecret(t, out)
}

func TestWrongPasswordSavesNothing(t *testing.T) { // test_wrong_password_saves_nothing
	env(t)
	code, out := run(t, nil, "a@example.com", "forkert", "login", "fake")
	if code != 1 || saved(t) != nil || !strings.Contains(out, "Tjek email og kodeord") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLogoutRemovesTheLogin(t *testing.T) { // test_logout_removes_the_login
	env(t)
	_ = store.KeychainSet("fake", "a@example.com", map[string]string{"token": "old"})
	if code, _ := run(t, nil, "", "", "logout", "fake"); code != 0 || saved(t) != nil {
		t.Fatalf("code=%d", code)
	}
}

func TestUnknownCommandOrStoreShowsUsage(t *testing.T) { // test_unknown_command_or_store_shows_usage
	env(t)
	for _, args := range [][]string{{"login", "netto"}, {"stjæl", "fake"}, {"login"}, {}} {
		code, out := run(t, nil, "", "", args...)
		if code != 2 || !strings.Contains(out, "Brug: handle-mcp login") || !strings.Contains(out, "Butikker: fake") {
			t.Fatalf("%v: code=%d out=%s", args, code, out)
		}
	}
}

func TestStoresInUsageAreSorted(t *testing.T) {
	var buf bytes.Buffer
	f := func() store.Store { return checkingStore{} }
	Run(bg, nil, map[string]Factory{"nemlig": f, "bilkatogo": f, "a": f}, Terminal{Stderr: &buf})
	if !strings.Contains(buf.String(), "Butikker: a, bilkatogo, nemlig") {
		t.Fatal(buf.String())
	}
}

func TestFailedReloginKeepsTheOldLogin(t *testing.T) { // test_failed_relogin_keeps_the_old_login
	env(t)
	_ = store.KeychainSet("fake", "a@example.com", map[string]string{"token": "old"})
	if code, _ := run(t, nil, "a@example.com", "forkert", "login", "fake"); code != 1 {
		t.Fatal("must fail")
	}
	if l := saved(t); l == nil || l.Session["token"] != "old" {
		t.Fatal("the old login changed")
	}
	if _, set := os.LookupEnv("HANDLE_FAKE_PASSWORD"); set { // the check leaves no env vars
		t.Fatal("env var set")
	}
}

func TestLogoutWithoutAKeychainSaysSo(t *testing.T) { // test_logout_without_a_keychain_says_so
	env(t)
	keyring.MockInitWithError(errors.New("no backend"))
	code, out := run(t, nil, "", "", "logout", "fake")
	if code != 1 || !strings.Contains(out, "Ingen nøglering") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLoginFromEnvWritesAnOwnerOnlyFile(t *testing.T) { // test_login_from_env_writes_an_owner_only_file
	dir := env(t)
	fromEnv(t, password)
	code, out := run(t, nil, "", "", "login", "fake", "--from-env")
	path := filepath.Join(dir, "fake.json")
	if code != 0 || !strings.Contains(out, path) {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, dir: 0o700} {
			if info, err := os.Stat(p); err != nil || info.Mode().Perm() != want {
				t.Fatalf("%s: %v %v", p, info.Mode().Perm(), err)
			}
		}
	}
	os.Unsetenv("HANDLE_FAKE_EMAIL")
	os.Unsetenv("HANDLE_FAKE_PASSWORD")
	l, err := store.LoadLogin(bg, "fake")
	if err != nil || l.Email != "a@example.com" || l.Session["token"] != token {
		t.Fatalf("%v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), password) {
		t.Fatal("password in the file")
	}
	assertNoSecret(t, out)
}

func TestLoginFromEnvWithAWrongPasswordSavesNothing(t *testing.T) {
	dir := env(t) // test_login_from_env_with_a_wrong_password_saves_nothing
	fromEnv(t, "forkert")
	code, out := run(t, nil, "", "", "login", "fake", "--from-env")
	if _, err := os.Stat(filepath.Join(dir, "fake.json")); code != 1 || err == nil ||
		!strings.Contains(out, "Tjek email og kodeord") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLoginFromEnvWithoutEnvVarsSaysWhich(t *testing.T) { // test_login_from_env_without_env_vars_says_which
	env(t)
	code, out := run(t, nil, "", "", "login", "fake", "--from-env")
	if code != 1 || !strings.Contains(out, "HANDLE_FAKE_EMAIL") || !strings.Contains(out, "Intet er gemt.") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLogoutRemovesTheFileToo(t *testing.T) { // test_logout_removes_the_file_too
	dir := env(t)
	_, _ = store.ConfigSet("fake", "a@example.com", map[string]string{"token": "old"})
	code, out := run(t, nil, "", "", "logout", "fake")
	if _, err := os.Stat(filepath.Join(dir, "fake.json")); code != 0 || err == nil ||
		!strings.Contains(out, "slettet fra config-filen.") { // one line, not two
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLogoutRemovesFileAndKeychainInOneLine(t *testing.T) { // test_logout_removes_file_and_keychain_in_one_line
	env(t)
	old := map[string]string{"token": "old"}
	_, _ = store.ConfigSet("fake", "a@example.com", old)
	_ = store.KeychainSet("fake", "a@example.com", old)
	code, out := run(t, nil, "", "", "logout", "fake")
	if code != 0 || !strings.Contains(out, "slettet fra config-filen og nøgleringen.") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLogoutFileOnlyWithoutAKeychain(t *testing.T) { // test_logout_file_only_without_a_keychain
	env(t)
	keyring.MockInitWithError(errors.New("no backend"))
	_, _ = store.ConfigSet("fake", "a@example.com", map[string]string{"token": "old"})
	code, out := run(t, nil, "", "", "logout", "fake")
	if code != 0 || !strings.Contains(out, "slettet fra config-filen.") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLogoutWithNothingSaved(t *testing.T) {
	env(t)
	if code, out := run(t, nil, "", "", "logout", "fake"); code != 0 || !strings.Contains(out, "Der var intet gemt login.") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLoginFromEnvTightensAnExistingWideFile(t *testing.T) { // test_login_from_env_tightens_an_existing_wide_file
	dir := env(t)
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "fake.json")
	_ = os.WriteFile(path, []byte(`{"email": "old@example.com", "password": "gammelt-og-langt-kodeord"}`), 0o644)
	_ = os.Chmod(path, 0o644)
	fromEnv(t, password)
	if code, _ := run(t, nil, "", "", "login", "fake", "--from-env"); code != 0 {
		t.Fatal("must work")
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"email":"a@example.com","session":{"token":"`+token+`"}}` {
		t.Fatalf("file: %s", data) // no old tail, no password
	}
}

func TestLoginFromEnvWarnsWhenTheKeychainWins(t *testing.T) { // test_login_from_env_warns_when_the_keychain_wins
	env(t)
	_ = store.KeychainSet("fake", "old@example.com", map[string]string{"token": "old"})
	fromEnv(t, password)
	code, out := run(t, nil, "", "", "login", "fake", "--from-env")
	if code != 0 || !strings.Contains(out, "nøgleringen har også et login") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestLoginFromEnvSurvivesABrokenKeychainEntry(t *testing.T) { // test_login_from_env_survives_a_broken_keychain_entry
	dir := env(t)
	_ = keyring.Set(store.KeychainService, "fake", "not json")
	fromEnv(t, password)
	code, out := run(t, nil, "", "", "login", "fake", "--from-env")
	if _, err := os.Stat(filepath.Join(dir, "fake.json")); code != 0 || err != nil ||
		!strings.Contains(out, "nøgleringen har også et login") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestNoTerminalSavesNothingAndNeverEchoes(t *testing.T) {
	env(t)
	for name, term := range map[string]Terminal{
		"not a terminal": {In: strings.NewReader("a@example.com\n"), IsTerminal: false,
			ReadPassword: func() (string, error) { t.Fatal("must not read"); return "", nil }},
		"read fails": {In: strings.NewReader("a@example.com\n"), IsTerminal: true,
			ReadPassword: func() (string, error) { return password, errors.New("The handle is invalid") }},
	} {
		var buf bytes.Buffer
		term.Stderr = &buf
		code := Run(bg, []string{"login", "fake"}, factories(nil), term)
		out := buf.String()
		if code != 1 || saved(t) != nil || !strings.Contains(out, "PowerShell") || !strings.Contains(out, "--from-env") {
			t.Fatalf("%s: code=%d out=%s", name, code, out)
		}
		assertNoSecret(t, out)
	}
}

func TestEmptyAnswersSaveNothing(t *testing.T) {
	env(t)
	if code, out := run(t, nil, "", password, "login", "fake"); code != 1 || !strings.Contains(out, "skal udfyldes") || saved(t) != nil {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestStdinIsNotSwallowed(t *testing.T) {
	env(t)
	in := strings.NewReader("a@example.com\nREST")
	var buf bytes.Buffer
	Run(bg, []string{"login", "fake"}, factories(nil), Terminal{In: in, Stderr: &buf, IsTerminal: true,
		ReadPassword: func() (string, error) { return password, nil }})
	if rest, _ := io.ReadAll(in); string(rest) != "REST" {
		t.Fatalf("left %q", rest)
	}
}

func TestSecretsNeverReachStderr(t *testing.T) {
	// A store that fails with text holding the password and a URL token, a keychain that fails,
	// a session too big, and the good paths: stderr never shows the password or the token.
	leaky := fmt.Errorf("Post https://x.test/login?pw=%s&t=%s: boom", password, token)
	env(t)
	for _, tc := range []struct {
		name string
		fail error
		init func()
	}{
		{"store error with secrets", leaky, nil},
		{"keychain fails", nil, func() { keyring.MockInitWithError(errors.New("denied " + token + password)) }},
		{"too big", nil, func() { keyring.MockInitWithError(keyring.ErrSetDataTooBig) }},
		{"good", nil, nil},
	} {
		keyring.MockInit()
		if tc.init != nil {
			tc.init()
		}
		code, out := run(t, tc.fail, "a@example.com", password, "login", "fake")
		assertNoSecret(t, out)
		if tc.name != "good" && code != 1 {
			t.Fatalf("%s: code=%d", tc.name, code)
		}
		keyring.MockInit()
		fromEnv(t, password)
		_, out = run(t, tc.fail, "", "", "login", "fake", "--from-env")
		assertNoSecret(t, out)
	}
}

func assertNoSecret(t *testing.T, out string) {
	t.Helper()
	for _, s := range []string{password, token, "SECRET"} {
		if strings.Contains(out, s) {
			t.Fatalf("stderr shows a secret (%q):\n%s", s, out)
		}
	}
}
