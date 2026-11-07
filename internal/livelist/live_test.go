package livelist

// A fixed list of ~50 everyday items, priced live in every store: the port of
// upstream/tests/test_live_list.py. The list itself stays in tests/live_list.json.
//
// The MCP finds and counts, the AI model picks. So an item fails only when no candidate fits: the
// right product never reached the model. A wrong suggestion (or øko too dear) is a warning,
// printed with "!", because the model can still pick another candidate.
//
// Run, in PowerShell, from the repo root (the passwords are typed, never put in a file):
//
//	$env:HANDLE_LIVE = "1"
//	$env:HANDLE_NEMLIG_EMAIL = Read-Host "nemlig email"
//	$pw = Read-Host "nemlig password" -AsSecureString
//	$env:HANDLE_NEMLIG_PASSWORD = [System.Net.NetworkCredential]::new("", $pw).Password
//	$env:HANDLE_BILKATOGO_EMAIL = Read-Host "BilkaToGo email"
//	$pw = Read-Host "BilkaToGo password" -AsSecureString
//	$env:HANDLE_BILKATOGO_PASSWORD = [System.Net.NetworkCredential]::new("", $pw).Password
//	go test -count=1 -v -timeout 30m -run Live ./internal/livelist/
//	Remove-Item Env:HANDLE_LIVE, Env:HANDLE_NEMLIG_EMAIL, Env:HANDLE_NEMLIG_PASSWORD, Env:HANDLE_BILKATOGO_EMAIL, Env:HANDLE_BILKATOGO_PASSWORD
//
// Or have run `handle-mcp login <store>` (a saved session is read from the keychain). -v shows the
// per-item report. Only reads: the guards in guards_test.go refuse every write. The report holds
// items, product names, prices and verdicts, never a pickup store name or an address.

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
	"github.com/McAndersDK/handle-mcp-go/internal/store/bilkatogo"
	"github.com/McAndersDK/handle-mcp-go/internal/store/nemlig"
	"github.com/McAndersDK/handle-mcp-go/internal/store/rema1000"
)

// --- live ----------------------------------------------------------------------------------------

// liveStore is the real store behind the guards: it reads only, and SetQuantities fails.
func liveStore(t *testing.T, name string) store.Store {
	t.Helper()
	if os.Getenv("HANDLE_LIVE") != "1" {
		t.Skip("set HANDLE_LIVE=1 to run against the real stores")
	}
	switch name {
	case "nemlig":
		return noWrites{Store: nemlig.New(&http.Client{Transport: nemligGuard{tb: t, inner: http.DefaultTransport}}), tb: t}
	case "bilkatogo":
		return noWrites{Store: bilkatogo.New(&http.Client{Transport: bilkaGuard{tb: t, inner: http.DefaultTransport}}), tb: t}
	case "rema1000":
		return noWrites{Store: rema1000.New(&http.Client{Transport: remaGuard{tb: t, inner: http.DefaultTransport}}), tb: t}
	}
	t.Fatalf("unknown store %q", name)
	return nil
}

func TestLiveFixedList(t *testing.T) {
	if os.Getenv("HANDLE_LIVE") != "1" {
		t.Skip("set HANDLE_LIVE=1 to run against the real stores")
	}
	list := mustList(t)
	for _, name := range storeNames {
		t.Run(name, func(t *testing.T) {
			s := liveStore(t, name)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
			defer cancel()
			msg, err := priceList(ctx, name, s, itemsFor(list, name), t.Logf)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if msg != "" {
				t.Error(msg)
			}
		})
	}
}
