// Package safety holds the tests that keep CLAUDE.md rule 1 (the human always pays) and the
// stdout rule: the code can never order, pay or change the address, and never prints to stdout.
package safety

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forbidden are endpoints and words that must never appear in the code (upstream/tests/test_safety.py).
var forbidden = []string{
	`PlaceOrder`,
	`Checkout/`,
	`GetCreditCards`,
	`GetCardsFees`,
	`/payment`, // also covers /payments (REMA 1000 payment jobs)
	`SetDeliveryAddress`,
	`SetDeliveryTime`,
	`UpdateDeliveryTime`, // nemlig: also TryUpdateDeliveryTime; chooses a slot
	`updateTimeslot`,
	`SetDeliveryDate`, // BilkaToGo: chooses a slot
	`ReserveTimeslot`,
	`ChangeAddress`,
	`EmptyCart`,
	`ClearBasket`, // nemlig: empties the whole cart
	`CancelOrder`,
	`AddRecipeToBasket`, // nemlig: fills the cart without the human's yes
	// REMA 1000 (docs/adr/0008): the only legal writes are to today's own shopping list.
	`v3/cart`,          // the cart, with store, time and coupon
	`v3/jobs`,          // payments
	`vendor-baskets`,   // imports a vendor's basket into a list
	`checkoutCallback`, // the payment's way back
	`primary\\?"\s*:\s*true|\bPrimary\s*:\s*true`, // makes a list primary, in JSON or a Go literal; the adapter only ever sends false
	`MethodDelete|(?-i:"DELETE")`,                 // deleting a list, in any form (the adapter removes lines with a PATCH)
}

// stdoutUses are ways to write to stdout. stdout is the JSON-RPC stream.
var stdoutUses = []string{`\bfmt\.Print`, `\bprintln\(`, `\bprint\(`, `os\.Stdout`}

type hit struct{ File, Pattern string }

// scan returns every file under the roots (non-test .go files only) that matches a pattern.
func scan(roots []string, patterns []string, caseInsensitive bool) ([]hit, error) {
	var res []*regexp.Regexp
	for _, p := range patterns {
		if caseInsensitive {
			p = "(?i)" + p
		}
		res = append(res, regexp.MustCompile(p))
	}
	var hits []hit
	files, err := goFiles(roots)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for i, re := range res {
			if re.Match(data) {
				hits = append(hits, hit{path, patterns[i]})
			}
		}
	}
	return hits, nil
}

// goFiles are the non-test .go files under the roots: the files scan reads, whatever build tags they have.
func goFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// codeRoots are the Go code folders. upstream/ (the Python mirror) is never scanned.
func codeRoots(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	return []string{filepath.Join(root, "cmd"), filepath.Join(root, "internal")}
}

// repoRoot walks up from the test's folder to go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestForbiddenEndpointsNotInCode(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "store", "store.go")); err != nil {
		t.Fatalf("scanning the wrong folder: %v", err)
	}
	hits, err := scan(codeRoots(t), forbidden, true)
	if err != nil || len(hits) > 0 {
		t.Fatalf("forbidden words found: %v (err %v)", hits, err)
	}
}

func TestNothingWritesToStdout(t *testing.T) {
	hits, err := scan(codeRoots(t), stdoutUses, false)
	if err != nil || len(hits) > 0 {
		t.Fatalf("stdout use found: %v (err %v)", hits, err)
	}
}

// The binary (cmd/handle-mcp) and the server write the answers, so both must be under the scans.
func TestScansCoverTheCommandAndTheServer(t *testing.T) {
	root := repoRoot(t)
	files, err := goFiles(codeRoots(t))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		rel = filepath.ToSlash(rel)
		for _, want := range []string{"cmd/handle-mcp/main.go", "internal/server/out.go", "internal/server/server.go", "internal/server/registry_e2e.go"} {
			found[want] = found[want] || rel == want
		}
	}
	for want, ok := range found {
		if !ok {
			t.Errorf("%s is not scanned", want)
		}
	}
	if len(found) != 4 {
		t.Errorf("checked %d files", len(found))
	}
}

func TestGitignoreKeepsSecretsOut(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{".env", "*.secret.json"} {
		if !strings.Contains(string(data), entry) {
			t.Errorf(".gitignore lacks %s", entry)
		}
	}
}

// The scanner must fail when a forbidden word is planted: prove it.
func TestScanFindsPlantedWords(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("clean.go", "package x\n\nfunc F() {}\n")
	write("planted.go", "package x\n\nconst u = \"/webapi/basket/placeorder\"\n") // other case
	write("planted_test.go", "package x\n\nconst u = \"PlaceOrder\"\n")           // tests are skipped
	write("printer.go", "package x\n\nfunc F() { fmt.Println(1) }\n")

	hits, err := scan([]string{dir}, forbidden, true)
	if err != nil || len(hits) != 1 || filepath.Base(hits[0].File) != "planted.go" || hits[0].Pattern != "PlaceOrder" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	hits, err = scan([]string{dir}, stdoutUses, false)
	if err != nil || len(hits) != 1 || filepath.Base(hits[0].File) != "printer.go" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	for _, w := range []string{`println(x)`, `os.Stdout`, `print(x)`} {
		write("printer.go", "package x\n\nfunc F() { "+w+" }\n")
		if hits, _ := scan([]string{dir}, stdoutUses, false); len(hits) == 0 {
			t.Errorf("%s not found", w)
		}
	}
	write("printer.go", "package x\n\nfunc F() { _ = fmt.Sprint(1) + log.Print }\n")
	if hits, _ := scan([]string{dir}, stdoutUses, false); len(hits) != 0 {
		t.Errorf("false positive: %v", hits)
	}
}

func TestForbiddenListIsComplete(t *testing.T) {
	// The Python list has 16 entries and REMA 1000 added 6 (docs/adr/0008); a shorter list means a
	// word was dropped.
	if len(forbidden) != 22 {
		t.Fatalf("%d forbidden words, want 22", len(forbidden))
	}
}

// The REMA 1000 words catch what they are meant to, and not the adapter's own legal calls
// (a PATCH of a list's lines, a primary flag that is false, a delete flag on a line).
func TestRemaWordsCatchOnlyTheForbiddenCalls(t *testing.T) {
	dir := t.TempDir()
	check := func(body string) []hit {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\n"+body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		hits, err := scan([]string{dir}, forbidden, true)
		if err != nil {
			t.Fatal(err)
		}
		return hits
	}
	for _, bad := range []string{
		`const a = "v3/cart/12"`,
		`const a = "v3/jobs/5/payments"`,
		`const a = "api/v3/vendor-baskets/x/items"`,
		`const a = "checkoutCallback"`,
		"const a = `{\"primary\": true}`",
		`const a = "{\"primary\":true}"`,
		`var c = call{method: http.MethodDelete}`,
		`var c = request{Method: "DELETE", URL: u}`,
		`var c = call{Method: "DELETE"}`,
		`var c = call{method: "DELETE"}`,
		`var m = "DELETE"`,
		`var l = newList{Primary: true}`,
		`var l = newList{Primary:true}`,
		`var l = newList{Name: n, Primary   :   true, Items: nil}`,
	} {
		if len(check(bad)) == 0 {
			t.Errorf("not caught: %s", bad)
		}
	}
	for _, good := range []string{
		"type body struct {\n\tPrimary bool `json:\"primary\"`\n}",
		"type gone struct {\n\tDelete bool `json:\"delete\"`\n}",
		`const a = "v2/shoppinglists/1"`,
		`var c = request{Method: "PATCH", URL: u}`,
		`var l = newList{Primary: false}`,
		`var r = removeItem{ID: id, Delete: true}`,
	} {
		if hits := check(good); len(hits) != 0 {
			t.Errorf("false positive for %s: %v", good, hits)
		}
	}
}
