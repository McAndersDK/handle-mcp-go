package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rule struct {
	match string
	out   string
	err   error
}

// fakeRunner answers the first rule whose text is inside "name args", and
// records every call.
type fakeRunner struct {
	rules []rule
	calls []string
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	for _, r := range f.rules {
		if strings.Contains(line, r.match) {
			return r.out, r.err
		}
	}
	return "", nil
}

func (f *fakeRunner) ran(sub string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func newEnv(f *fakeRunner, root string, dry bool) (*Env, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Env{R: f, Out: &buf, Root: root, DryRun: dry, Sleep: func(time.Duration) {}}, &buf
}

const origin = "https://github.com/McAndersDK/handle-mcp-go.git"

func assetsJSON(v Version) string {
	var a []map[string]string
	for _, n := range expectedAssets(v) {
		a = append(a, map[string]string{"name": n})
	}
	b, _ := json.Marshal(map[string]any{"assets": a})
	return string(b)
}

func finishRules(prState string, ancestor error) []rule {
	pr := `[{"number":4,"state":"` + prState + `","url":"https://example.test/pull/4","mergeCommit":{"oid":"abc123"}}]`
	return []rule{
		{"remote get-url", origin, nil},
		{"ls-remote --tags", "1\trefs/tags/v1.2.0\n2\trefs/tags/v1.2.0^{}", nil},
		{"pr list", pr, nil},
		{"merge-base", "", ancestor},
		{"run list", `[{"databaseId":9,"headSha":"abc123","status":"completed","conclusion":"success"}]`, nil},
		{"run view", `{"conclusion":"success","jobs":[{"name":"goreleaser","conclusion":"success"}]}`, nil},
		{"release view", assetsJSON(Version{1, 3, 0}), nil},
		{"rev-parse --verify", "", errors.New("no such ref")},
	}
}

func TestParseVersion(t *testing.T) {
	for _, bad := range []string{"1.2.3", "v1.2", "v1.2.3.4", "v1.2.x", "v01.2.3", "v1.2.3-rc1", " v1.2.3", ""} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	v, err := ParseVersion("v1.12.3")
	if err != nil || v != (Version{1, 12, 3}) || v.String() != "v1.12.3" {
		t.Errorf("got %v %v", v, err)
	}
}

func TestGreater(t *testing.T) {
	cases := []struct {
		a, b Version
		want bool
	}{
		{Version{1, 3, 0}, Version{1, 2, 0}, true},
		{Version{1, 10, 0}, Version{1, 9, 9}, true},
		{Version{2, 0, 0}, Version{1, 99, 99}, true},
		{Version{1, 2, 0}, Version{1, 2, 0}, false},
		{Version{1, 2, 0}, Version{1, 2, 1}, false},
	}
	for _, c := range cases {
		if c.a.Greater(c.b) != c.want {
			t.Errorf("%v > %v should be %v", c.a, c.b, c.want)
		}
	}
}

func TestLatestTag(t *testing.T) {
	out := "a\trefs/tags/v1.9.0\nb\trefs/tags/v1.10.0\nc\trefs/tags/v1.10.0^{}\nd\trefs/tags/nightly\ne\trefs/tags/v2.0.0-rc1"
	got, ok := Latest(ParseTags(out))
	if !ok || got != (Version{1, 10, 0}) {
		t.Errorf("got %v %v", got, ok)
	}
	if _, ok := Latest(ParseTags("")); ok {
		t.Error("no tags should give no latest")
	}
}

func TestPrepareRejectsOldVersion(t *testing.T) {
	f := &fakeRunner{rules: []rule{
		{"remote get-url", origin, nil},
		{"ls-remote --tags", "1\trefs/tags/v1.2.0", nil},
	}}
	e, _ := newEnv(f, t.TempDir(), false)
	if err := e.prepare("v1.1.9"); err == nil || !strings.Contains(err.Error(), "greater") {
		t.Errorf("got %v", err)
	}
	if f.ran("switch") || f.ran("push") {
		t.Error("nothing should be written")
	}
}

func TestBumpText(t *testing.T) {
	in := "go install example/cmd/handle-mcp@v1.2.0\n" +
		"[the v1.2.0 release](https://github.com/x/y/releases/tag/v1.2.0)\n" +
		"pin `@v1.2.0`. Keep v1.2.0 here, and @v1.2.01 and @v1.2.0.\n"
	got := bumpText(in, Version{1, 2, 0}, Version{1, 3, 0})
	want := "go install example/cmd/handle-mcp@v1.3.0\n" +
		"[the v1.3.0 release](https://github.com/x/y/releases/tag/v1.3.0)\n" +
		"pin `@v1.3.0`. Keep v1.2.0 here, and @v1.2.01 and @v1.3.0.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func writeTree(t *testing.T) string {
	root := t.TempDir()
	files := map[string]string{
		"README.md":                "pin @v1.2.0\n",
		"docs/setup.md":            "[the v1.2.0 release](https://h/releases/tag/v1.2.0)\n",
		"docs/tools.md":            "no pin here\n",
		"docs/adr/0001-history.md": "history says @v1.2.0\n",
	}
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBumpPinsLeavesAdrAlone(t *testing.T) {
	root := writeTree(t)
	changed, err := bumpPins(root, Version{1, 2, 0}, Version{1, 3, 0}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "README.md,docs/setup.md" {
		t.Errorf("changed %v", changed)
	}
	adr, _ := os.ReadFile(filepath.Join(root, "docs", "adr", "0001-history.md"))
	if string(adr) != "history says @v1.2.0\n" {
		t.Errorf("adr was edited: %q", adr)
	}
}

func TestBumpPinsDryRunWritesNothing(t *testing.T) {
	root := writeTree(t)
	changed, _ := bumpPins(root, Version{1, 2, 0}, Version{1, 3, 0}, true)
	b, _ := os.ReadFile(filepath.Join(root, "README.md"))
	if len(changed) != 2 || string(b) != "pin @v1.2.0\n" {
		t.Errorf("dry run wrote: %v %q", changed, b)
	}
}

func TestPRBody(t *testing.T) {
	got := prBody(Version{1, 2, 0}, []string{"Fix a", "", " Add b "})
	want := "Changes since v1.2.0:\n\n- Fix a\n- Add b\n"
	if got != want {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(prBody(Version{1, 2, 0}, nil), "No commits") {
		t.Error("empty list should say so")
	}
}

func TestFinishRefusesUnmergedPR(t *testing.T) {
	f := &fakeRunner{rules: finishRules("OPEN", nil)}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "not MERGED") {
		t.Errorf("got %v", err)
	}
	if f.ran("tag -a") || f.ran("push") {
		t.Error("nothing should be written")
	}
}

func TestFinishRefusesExistingTag(t *testing.T) {
	f := &fakeRunner{rules: append([]rule{{"ls-remote --tags", "1\trefs/tags/v1.3.0", nil}}, finishRules("MERGED", nil)...)}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "already exists on origin") {
		t.Errorf("got %v", err)
	}
	f = &fakeRunner{rules: append([]rule{{"tag --list", "v1.3.0", nil}}, finishRules("MERGED", nil)...)}
	e, _ = newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "locally") {
		t.Errorf("got %v", err)
	}
	if f.ran("tag -a") {
		t.Error("must not tag")
	}
}

func TestFinishRefusesMergeCommitNotOnMain(t *testing.T) {
	f := &fakeRunner{rules: finishRules("MERGED", &RunError{Code: 1})}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "not on origin/main") {
		t.Errorf("got %v", err)
	}
	if f.ran("tag -a") {
		t.Error("must not tag")
	}
}

func TestFinishRefusesWrongOrigin(t *testing.T) {
	f := &fakeRunner{rules: []rule{{"remote get-url", "https://github.com/NJohansen/handle-mcp.git", nil}}}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Errorf("got %v", err)
	}
}

func TestFinishFailsOnMissingAssets(t *testing.T) {
	rules := append([]rule{{"release view", `{"assets":[{"name":"checksums.txt"}]}`, nil}}, finishRules("MERGED", nil)...)
	f := &fakeRunner{rules: rules}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err == nil || !strings.Contains(err.Error(), "misses assets") {
		t.Errorf("got %v", err)
	}
}

func TestFinishTagsExactCommit(t *testing.T) {
	f := &fakeRunner{rules: finishRules("MERGED", nil)}
	e, _ := newEnv(f, "", false)
	if err := e.finish("v1.3.0"); err != nil {
		t.Fatal(err)
	}
	if !f.ran("git tag -a v1.3.0 -m Release v1.3.0 abc123") || !f.ran("git push origin v1.3.0") {
		t.Errorf("calls: %v", f.calls)
	}
	checkAllCalls(t, f)
}

func TestFinishDryRunWritesNothing(t *testing.T) {
	f := &fakeRunner{rules: finishRules("MERGED", nil)}
	e, buf := newEnv(f, "", true)
	if err := e.finish("v1.3.0"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"tag -a", "push", "switch", "pull"} {
		if f.ran(w) {
			t.Errorf("dry run ran %s", w)
		}
	}
	if !strings.Contains(buf.String(), "dry-run: git tag -a v1.3.0") {
		t.Errorf("output: %s", buf.String())
	}
}

func TestPrepareFullRun(t *testing.T) {
	root := writeTree(t)
	rules := []rule{
		{"remote get-url", origin, nil},
		{"ls-remote --tags", "1\trefs/tags/v1.2.0", nil},
		{"rev-parse --abbrev-ref", "main", nil},
		{"rev-parse main", "s1", nil},
		{"rev-parse origin/main", "s1", nil},
		{"log v1.2.0..HEAD", "Fix a\nAdd b", nil},
		{"pr create", "https://example.test/pull/5", nil},
		{"rev-parse HEAD", "s2", nil},
		{"run list", `[{"databaseId":7,"headSha":"s2","status":"completed","conclusion":"success"}]`, nil},
		{"run view", `{"conclusion":"success","jobs":[{"name":"go (ubuntu-latest)","conclusion":"success"}]}`, nil},
	}
	f := &fakeRunner{rules: rules}
	e, buf := newEnv(f, root, false)
	if err := e.prepare("v1.3.0"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"git switch -c chore/release-v1.3.0",
		"git commit -m Pin the install commands to v1.3.0",
		"git push -u origin chore/release-v1.3.0",
		"--title Release v1.3.0",
		"- Fix a\n- Add b",
	} {
		if !f.ran(w) {
			t.Errorf("missing call %q in %v", w, f.calls)
		}
	}
	if !strings.Contains(buf.String(), "go run ./tools/release finish v1.3.0") {
		t.Errorf("output: %s", buf.String())
	}
	checkAllCalls(t, f)
}

func TestPrepareFailsWhenNoPinChanges(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "README.md"), []byte("nothing\n"), 0o644)
	f := &fakeRunner{rules: []rule{
		{"remote get-url", origin, nil},
		{"ls-remote --tags", "1\trefs/tags/v1.2.0", nil},
		{"rev-parse --abbrev-ref", "main", nil},
		{"rev-parse main", "s1", nil},
		{"rev-parse origin/main", "s1", nil},
	}}
	e, _ := newEnv(f, root, false)
	if err := e.prepare("v1.3.0"); err == nil || !strings.Contains(err.Error(), "nothing to bump") {
		t.Errorf("got %v", err)
	}
	if f.ran("commit") || f.ran("push") {
		t.Error("must not commit or push")
	}
}

func TestPrepareStopsOnUncommittedChanges(t *testing.T) {
	f := &fakeRunner{rules: []rule{
		{"remote get-url", origin, nil},
		{"ls-remote --tags", "1\trefs/tags/v1.2.0", nil},
		{"rev-parse --abbrev-ref", "main", nil},
		{"status --porcelain", " M README.md", nil},
	}}
	e, _ := newEnv(f, t.TempDir(), false)
	if err := e.prepare("v1.3.0"); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("got %v", err)
	}
}

// checkAllCalls enforces the two rules over a whole run.
func checkAllCalls(t *testing.T, f *fakeRunner) {
	t.Helper()
	for _, c := range f.calls {
		if strings.Contains(c, "--force") || strings.HasPrefix(c, "git push") && strings.Contains(c, "--tags") {
			t.Errorf("forbidden flag in %q", c)
		}
		if strings.HasPrefix(c, "gh ") && !strings.Contains(c, "--repo McAndersDK/handle-mcp-go") {
			t.Errorf("gh call without --repo: %q", c)
		}
	}
}

func TestGuard(t *testing.T) {
	for _, args := range [][]string{{"push", "--force", "origin"}, {"push", "--tags"}, {"push", "-f"}, {"push", "--force-with-lease"}} {
		if guard("git", args) == nil {
			t.Errorf("git %v should be refused", args)
		}
	}
	if guard("gh", []string{"pr", "list"}) == nil {
		t.Error("gh without --repo should be refused")
	}
	if guard("gh", []string{"pr", "list", "--repo", "NJohansen/handle-mcp"}) == nil {
		t.Error("gh against upstream should be refused")
	}
	if err := guard("gh", []string{"pr", "list", "--repo", repo}); err != nil {
		t.Error(err)
	}
}
