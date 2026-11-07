package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// choosingStore is a store that needs one more choice (store.Chooser), like REMA 1000.
type choosingStore struct {
	store.Store
	listErr error
}

func (choosingStore) Name() string { return "pick" }
func (c choosingStore) Choices(ctx context.Context) (string, []store.Choice, error) {
	l, err := store.LoadLogin(ctx, "pick")
	if err != nil || l.Password != password {
		return "", nil, store.Errorf("Tjek email og kodeord.")
	}
	if c.listErr != nil {
		return "", nil, c.listErr
	}
	return "Vælg butik:", []store.Choice{{Key: "11", Label: "Butik A"}, {Key: "22", Label: "Butik B"}}, nil
}
func (choosingStore) Session() map[string]string { return map[string]string{"token": token} }
func (choosingStore) WithChoice(s map[string]string, c store.Choice) map[string]string {
	out := map[string]string{"store_id": c.Key}
	for k, v := range s {
		out[k] = v
	}
	return out
}

func runPick(t *testing.T, terminal bool, typed string, args ...string) (int, string) {
	t.Helper()
	var errBuf bytes.Buffer
	code := Run(bg, args, map[string]Factory{"pick": func() store.Store { return choosingStore{} }}, Terminal{
		In:           strings.NewReader("a@example.com\n" + typed),
		Stderr:       &errBuf,
		IsTerminal:   terminal,
		ReadPassword: func() (string, error) { return password, nil },
	})
	return code, errBuf.String()
}

func pickEnv(t *testing.T) {
	t.Helper()
	resetWorld(t)
	t.Setenv("HANDLE_CONFIG_DIR", t.TempDir())
	t.Setenv("HANDLE_PICK_STORE_ID", "")
	os.Unsetenv("HANDLE_PICK_STORE_ID")
}

func resetWorld(t *testing.T) { t.Helper(); env(t) }

func TestLoginAsksForTheChoiceAndSavesItWithTheSession(t *testing.T) {
	pickEnv(t)
	code, out := runPick(t, true, "2\n", "login", "pick")
	l, err := store.KeychainGet("pick")
	if code != 0 || err != nil || l == nil || l.Session["store_id"] != "22" || l.Session["token"] != token || l.Password != "" {
		t.Fatalf("code=%d login=%v err=%v\n%s", code, l, err, out)
	}
	if !strings.Contains(out, "1. Butik A") || !strings.Contains(out, "2. Butik B") {
		t.Errorf("the list was not shown:\n%s", out)
	}
	if strings.Contains(out, password) {
		t.Error("the password is in the output")
	}
}

func TestAnInvalidChoiceSavesNothing(t *testing.T) {
	pickEnv(t)
	code, out := runPick(t, true, "7\n", "login", "pick")
	if l, _ := store.KeychainGet("pick"); code != 1 || l != nil || !strings.Contains(out, "Ugyldigt valg") {
		t.Fatalf("code=%d saved=%v\n%s", code, l != nil, out)
	}
}

func TestStoreIdEnvChoosesWithoutAsking(t *testing.T) {
	pickEnv(t)
	t.Setenv("HANDLE_PICK_STORE_ID", "11")
	code, out := runPick(t, true, "", "login", "pick")
	l, _ := store.KeychainGet("pick")
	if code != 0 || l == nil || l.Session["store_id"] != "11" {
		t.Fatalf("code=%d\n%s", code, out)
	}
	t.Setenv("HANDLE_PICK_STORE_ID", "99")
	if code, out := runPick(t, true, "", "login", "pick"); code != 1 || !strings.Contains(out, "er ikke på listen") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

func TestFromEnvWithoutATerminalNeedsTheStoreId(t *testing.T) {
	pickEnv(t)
	t.Setenv("HANDLE_PICK_EMAIL", "a@example.com")
	t.Setenv("HANDLE_PICK_PASSWORD", password)
	code, out := runPick(t, false, "", "login", "pick", "--from-env")
	if code != 1 || !strings.Contains(out, "HANDLE_PICK_STORE_ID") || !strings.Contains(out, "22  Butik B") {
		t.Fatalf("code=%d\n%s", code, out)
	}
	t.Setenv("HANDLE_PICK_STORE_ID", "22")
	code, out = runPick(t, false, "", "login", "pick", "--from-env")
	if code != 0 {
		t.Fatalf("code=%d\n%s", code, out)
	}
	dir, _ := store.ConfigDir()
	data, err := os.ReadFile(dir + string(os.PathSeparator) + "pick.json")
	if err != nil || !strings.Contains(string(data), `"store_id":"22"`) || strings.Contains(string(data), password) {
		t.Fatalf("file: %s err %v", data, err)
	}
}
