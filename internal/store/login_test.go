package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

var bg = context.Background()

// setup gives a fresh in-memory keychain, a temp config dir and no HANDLE_FAKE_* env vars.
func setup(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := filepath.Join(t.TempDir(), "handle-mcp")
	t.Setenv("HANDLE_CONFIG_DIR", dir)
	t.Setenv("HANDLE_FAKE_EMAIL", "")
	t.Setenv("HANDLE_FAKE_PASSWORD", "")
	return dir
}

func writeFile(t *testing.T, dir, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fake.json")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantErr(t *testing.T, err error, sub string) {
	t.Helper()
	var se *StoreError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), sub) {
		t.Fatalf("want StoreError containing %q, got %v", sub, err)
	}
}

func TestLoginFromEnv(t *testing.T) { // test_login_from_env
	setup(t)
	t.Setenv("HANDLE_FAKE_EMAIL", "a@example.com")
	t.Setenv("HANDLE_FAKE_PASSWORD", "x")
	l, err := LoadLogin(bg, "fake")
	if err != nil || l.Email != "a@example.com" || l.Password != "x" || l.Session != nil {
		t.Fatalf("%v %v", l.Email, err)
	}
}

func TestLoginFileMustBePrivate(t *testing.T) { // test_login_file_must_be_private
	dir := setup(t)
	path := writeFile(t, dir, `{"email": "a@example.com", "password": "x"}`, 0o644)
	if runtime.GOOS != "windows" { // Windows has no mode bits to check (ADR 0004)
		_, err := LoadLogin(bg, "fake")
		wantErr(t, err, "chmod 600")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := LoadLogin(bg, "fake"); err != nil || l.Email != "a@example.com" {
		t.Fatalf("%v", err)
	}
}

func TestMissingLoginSaysHowToFix(t *testing.T) { // test_missing_login_says_how_to_fix + ..._names_the_headless_way
	setup(t)
	_, err := LoadLogin(bg, "fake")
	wantErr(t, err, "handle-mcp login fake")
	wantErr(t, err, "handle-mcp login fake --from-env")
	wantErr(t, err, "HANDLE_FAKE_EMAIL og _PASSWORD")
	if HasLogin(bg, "fake") {
		t.Fatal("HasLogin must be false")
	}
}

func TestKeychainBeatsFileAndEnvBeatsKeychain(t *testing.T) { // test_login_from_keychain_beats_file
	dir := setup(t)
	writeFile(t, dir, `{"email": "file@example.com", "password": "f"}`, 0o600)
	if err := KeychainSet("fake", "kc@example.com", map[string]string{"token": "k"}); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLogin(bg, "fake")
	if err != nil || l.Email != "kc@example.com" || l.Session["token"] != "k" || l.Password != "" {
		t.Fatalf("keychain: %v", err)
	}
	t.Setenv("HANDLE_FAKE_EMAIL", "env@example.com")
	t.Setenv("HANDLE_FAKE_PASSWORD", "e")
	if l, _ = LoadLogin(bg, "fake"); l.Email != "env@example.com" {
		t.Fatal("env must win")
	}
	// A login in ctx wins over all of them and touches nothing.
	ctx := WithLogin(bg, "fake", Login{Email: "ctx@example.com", Password: "c"})
	if l, _ = LoadLogin(ctx, "fake"); l.Email != "ctx@example.com" {
		t.Fatal("ctx must win")
	}
	if l, _ = LoadLogin(ctx, "other"); l.Email == "ctx@example.com" {
		t.Fatal("ctx login is for one store")
	}
}

func TestNoKeychainFallsBackToFile(t *testing.T) { // test_no_keychain_falls_back_to_file
	dir := setup(t)
	keyring.MockInitWithError(errors.New("no backend"))
	writeFile(t, dir, `{"email": "a@example.com", "password": "x"}`, 0o600)
	if l, err := LoadLogin(bg, "fake"); err != nil || l.Email != "a@example.com" {
		t.Fatalf("%v", err)
	}
	if saved, err := KeychainGet("fake"); saved != nil || err != nil {
		t.Fatal("a broken keychain is none")
	}
}

func TestBrokenKeychainEntry(t *testing.T) { // test_broken_keychain_entry_..., ..._surfaces_..., entry_without_session_or_password
	secrets := []string{"not json", `{"email": "a@example.com"}`, "[]",
		`{"email": "a@example.com", "session": {}}`, `{"session": {"t": "x"}}`,
		`{"email": null, "password": "x"}`, `{"email": "a@example.com", "password": ""}`}
	for _, secret := range secrets {
		setup(t)
		if err := keyring.Set(KeychainService, "fake", secret); err != nil {
			t.Fatal(err)
		}
		_, err := KeychainGet("fake")
		wantErr(t, err, "Ugyldigt login i nøgleringen")
		_, err = LoadLogin(bg, "fake")
		wantErr(t, err, "handle-mcp login fake")
	}
}

func TestAnOldSavedPasswordIsStillReadThenReplaced(t *testing.T) { // test_an_old_saved_password_is_still_read
	setup(t)
	if err := keyring.Set(KeychainService, "fake", `{"email": "a@example.com", "password": "rigtig"}`); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLogin(bg, "fake")
	if err != nil || l.Password != "rigtig" || l.Session != nil {
		t.Fatalf("old entry: %v", err)
	}
	SaveSession(bg, "fake", "a@example.com", map[string]string{"token": "new"})
	l, err = LoadLogin(bg, "fake")
	if err != nil || l.Session["token"] != "new" || l.Password != "" {
		t.Fatalf("after save: %v", err)
	}
	raw, _ := keyring.Get(KeychainService, "fake")
	if strings.Contains(raw, "rigtig") {
		t.Fatal("the password must be gone")
	}
}

func TestSaveSessionReplacesAnOldPasswordInTheFile(t *testing.T) { // test_save_session_replaces_...
	dir := setup(t)
	path := writeFile(t, dir, `{"email": "a@example.com", "password": "rigtig"}`, 0o600)
	SaveSession(bg, "fake", "a@example.com", map[string]string{"token": "new"})
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "rigtig") || !strings.Contains(string(data), "new") {
		t.Fatalf("file: %s", data)
	}
	assertMode(t, path, 0o600)
}

func TestSaveSessionNeverWritesWhenEnvOrCtxHoldTheLogin(t *testing.T) { // test_save_session_never_writes_...
	dir := setup(t)
	t.Setenv("HANDLE_FAKE_EMAIL", "a@example.com")
	t.Setenv("HANDLE_FAKE_PASSWORD", "rigtig")
	SaveSession(bg, "fake", "a@example.com", map[string]string{"token": "new"})
	t.Setenv("HANDLE_FAKE_PASSWORD", "")
	writeFile(t, dir, `{"email": "a@example.com", "password": "rigtig"}`, 0o600)
	SaveSession(WithLogin(bg, "fake", Login{Email: "e", Password: "p"}), "fake", "e", map[string]string{"token": "new"})
	data, _ := os.ReadFile(filepath.Join(dir, "fake.json"))
	if strings.Contains(string(data), "new") {
		t.Fatalf("written although the login came from elsewhere: %s", data)
	}
	if _, err := keyring.Get(KeychainService, "fake"); err == nil {
		t.Fatal("keychain written")
	}
}

func TestSaveSessionWarnsWithTheTypeOnly(t *testing.T) {
	setup(t)
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	// A broken entry makes the save fail: the warning names the type, never the text.
	_ = keyring.Set(KeychainService, "fake", "not json")
	SaveSession(bg, "fake", "a@example.com", map[string]string{"token": "tok-SECRET"})
	out := buf.String()
	if !strings.Contains(out, "could not save") || !strings.Contains(out, "StoreError") {
		t.Fatalf("log: %q", out)
	}
	for _, s := range []string{"SECRET", "a@example.com", "not json"} {
		if strings.Contains(out, s) {
			t.Fatalf("log leaks %q: %s", s, out)
		}
	}
}

func TestKeychainSetTooBigIsClear(t *testing.T) {
	setup(t)
	keyring.MockInitWithError(keyring.ErrSetDataTooBig)
	err := KeychainSet("fake", "a@example.com", map[string]string{"t": "x"})
	wantErr(t, err, "for stor")
	wantErr(t, err, "--from-env")
}

func TestKeychainDelete(t *testing.T) {
	setup(t)
	if ok, err := KeychainDelete("fake"); ok || err != nil {
		t.Fatalf("nothing saved: %v %v", ok, err)
	}
	_ = KeychainSet("fake", "a@example.com", map[string]string{"t": "x"})
	if ok, err := KeychainDelete("fake"); !ok || err != nil {
		t.Fatalf("saved: %v %v", ok, err)
	}
	keyring.MockInitWithError(errors.New("no backend"))
	_, err := KeychainDelete("fake")
	var ke *KeychainError
	if !errors.As(err, &ke) || strings.Contains(err.Error(), "no backend") {
		t.Fatalf("want a bare KeychainError, got %v", err)
	}
}

func TestConfigSetTightensAndTruncates(t *testing.T) { // test_login_from_env_tightens_an_existing_wide_file
	dir := setup(t)
	path := writeFile(t, dir, `{"email": "old@example.com", "password": "gammelt-og-langt-kodeord"}`, 0o644)
	got, err := ConfigSet("fake", "a@example.com", map[string]string{"token": "session-1"})
	if err != nil || got != path {
		t.Fatalf("%v %v", got, err)
	}
	assertMode(t, path, 0o600)
	assertMode(t, dir, 0o700)
	data, _ := os.ReadFile(path)
	if string(data) != `{"email":"a@example.com","session":{"token":"session-1"}}` {
		t.Fatalf("file: %s", data)
	}
	if removed, err := ConfigDelete("fake"); !removed || err != nil {
		t.Fatal("delete")
	}
	if removed, err := ConfigDelete("fake"); removed || err != nil {
		t.Fatal("second delete")
	}
}

func TestConfigDirDefaultsToDotConfigOnEveryOS(t *testing.T) {
	t.Setenv("HANDLE_CONFIG_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := ConfigDir()
	if err != nil || dir != filepath.Join(home, ".config", "handle-mcp") {
		t.Fatalf("%q %v", dir, err)
	}
}

func TestLoginNeverPrints(t *testing.T) {
	l := Login{Email: "a@example.com", Password: "pw-SECRET", Session: map[string]string{"t": "tok-SECRET"}}
	got := fmt.Sprintf("%v|%+v|%#v|%s", l, l, l, l)
	if strings.Contains(got, "SECRET") || strings.Contains(got, "example") {
		t.Fatalf("leak: %s", got)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" { // no mode bits (ADR 0004)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != want {
		t.Fatalf("%s: mode %v, want %v (%v)", path, info.Mode().Perm(), want, err)
	}
}
