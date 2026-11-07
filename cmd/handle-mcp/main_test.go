package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests build the binary and run it as a subprocess, the way an MCP client does. The binary
// with `-tags e2e` has two fake stores that fail (server/registry_e2e.go): no store is called and
// no login is read. The one without the tag has the real registry and is only asked what needs no store.

var (
	buildOnce sync.Map // tag -> func() string
	buildDir  string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "handle-mcp-e2e")
	if err != nil {
		panic(err)
	}
	buildDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func binary(t *testing.T, tags string) string {
	t.Helper()
	exe := filepath.Join(buildDir, "handle-mcp-"+strings.ReplaceAll(tags, ",", "-")+"x")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	once, _ := buildOnce.LoadOrStore(exe, sync.OnceValue(func() string {
		args := []string{"build", "-buildvcs=false", "-o", exe} // no git stamp: serverInfo says "dev"
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		out, err := exec.Command("go", append(args, ".")...).CombinedOutput()
		if err != nil {
			return "build failed: " + err.Error() + "\n" + string(out)
		}
		return ""
	}))
	if msg := once.(func() string)(); msg != "" {
		t.Fatal(msg)
	}
	return exe
}

// cleanEnv has no HANDLE_* variable, and a config folder that is empty: no login can be found.
func cleanEnv(t *testing.T) []string {
	env := []string{"HANDLE_CONFIG_DIR=" + t.TempDir()}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "HANDLE_") {
			env = append(env, kv)
		}
	}
	return env
}

type output struct {
	stdout []byte
	stderr string
	code   int
}

// start runs the binary and sends the lines on stdin. It keeps stdin open until ${want} responses
// have come (a server stops at the end of stdin), then closes it and waits for the exit.
func start(t *testing.T, exe string, args []string, want int, lines ...string) output {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Env = cleanEnv(t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	timer := time.AfterFunc(30*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	var stdout bytes.Buffer
	got := make(chan struct{}, 1000)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			stdout.Write(sc.Bytes())
			stdout.WriteByte(10)
			if bytes.Contains(sc.Bytes(), []byte(`"id"`)) {
				got <- struct{}{}
			}
		}
	}()
	for _, l := range lines {
		_, _ = io.WriteString(in, l+"\n")
	}
	for i := 0; i < want; i++ {
		select {
		case <-got:
		case <-done:
			i = want
		}
	}
	_ = in.Close()
	<-done
	err := cmd.Wait()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return output{stdout.Bytes(), stderr.String(), code}
}

const (
	initialize  = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}`
	initialized = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
)

func callTool(id int, name, args string) string {
	return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// messages checks that stdout is JSON-RPC and nothing else, and returns the responses by id.
func messages(t *testing.T, stdout []byte) map[int]map[string]any {
	t.Helper()
	byID := map[int]map[string]any{}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	lines := 0
	for sc.Scan() {
		lines++
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line %d is not JSON-RPC: %.200q", lines, sc.Text())
		}
		if id, ok := m["id"].(float64); ok {
			byID[int(id)] = m
		}
	}
	if lines == 0 {
		t.Fatal("nothing on stdout")
	}
	return byID
}

func resultOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	res, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", m)
	}
	return res
}

func textOf(t *testing.T, m map[string]any) (string, bool) {
	t.Helper()
	res := resultOf(t, m)
	content := res["content"].([]any)
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestServesOnlyJSONRPCOnStdoutWhenStoresFail(t *testing.T) {
	r := start(t, binary(t, "e2e"), nil, 9,
		initialize, initialized,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		callTool(3, "search_products", `{"query":"mælk"}`),
		callTool(4, "get_cart", `{}`),
		callTool(5, "get_cart", `{"store":"nope"}`),
		callTool(6, "compare_basket", `{"items":["mælk 1 l"]}`),
		callTool(7, "set_cart_quantities", `{"store":"refuses","items":{"1":"mange"}}`),
		callTool(8, "no_such_tool", `{}`),
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
	)
	if r.code != 0 {
		t.Errorf("exit code %d, stderr:\n%s", r.code, r.stderr)
	}
	byID := messages(t, r.stdout)
	if len(byID) != 9 {
		t.Fatalf("%d responses, want 9: %v", len(byID), r.stderr)
	}
	if info := resultOf(t, byID[1])["serverInfo"].(map[string]any); info["name"] != "handle" || info["version"] != "dev" {
		t.Errorf("serverInfo %v", info)
	}
	if n := len(resultOf(t, byID[2])["tools"].([]any)); n != 13 {
		t.Errorf("%d tools", n)
	}
	text, isErr := textOf(t, byID[3])
	var search map[string]map[string]string
	if err := json.Unmarshal([]byte(text), &search); err != nil || isErr {
		t.Fatalf("search_products: %v %v %s", err, isErr, text)
	}
	if search["refuses"]["error"] != "refuses: API'et svarer 500" || !strings.HasPrefix(search["crashes"]["error"], "crashes: uventet fejl (string)") {
		t.Errorf("search_products: %v", search)
	}
	text, isErr = textOf(t, byID[4])
	if isErr || !strings.Contains(text, "uventet fejl") {
		t.Errorf("get_cart without a store: %v %s", isErr, text)
	}
	if text, isErr = textOf(t, byID[5]); !isErr || text != "Ukendt butik 'nope'. Kendte butikker: crashes, refuses." {
		t.Errorf("unknown store: %v %q", isErr, text)
	}
	if text, isErr = textOf(t, byID[6]); isErr || !strings.Contains(text, "Ingen butik kunne prise listen.") {
		t.Errorf("compare_basket: %v %s", isErr, text)
	}
	if text, isErr = textOf(t, byID[7]); !isErr || text != "Argumentet 'items.1' skal være et heltal." {
		t.Errorf("bad argument: %v %q", isErr, text)
	}
	if byID[8]["error"] == nil {
		t.Errorf("an unknown tool must be a JSON-RPC error: %v", byID[8])
	}
	if !strings.Contains(r.stderr, "store panicked") { // logs go to stderr, which is where they belong
		t.Errorf("stderr lacks the panic log:\n%s", r.stderr)
	}
}

func TestRealRegistryNeedsNoStoreForAToolListOrAnUnknownStore(t *testing.T) {
	r := start(t, binary(t, ""), nil, 3, initialize, initialized,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		callTool(3, "get_cart", `{"store":"nope"}`),
	)
	byID := messages(t, r.stdout)
	if n := len(resultOf(t, byID[2])["tools"].([]any)); n != 13 {
		t.Errorf("%d tools", n)
	}
	if text, isErr := textOf(t, byID[3]); !isErr || text != "Ukendt butik 'nope'. Kendte butikker: bilkatogo, nemlig." {
		t.Errorf("%v %q", isErr, text)
	}
}

func TestResolveVersion(t *testing.T) {
	info := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: v}}
	}
	cases := []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"ldflags wins over the module version", "2.0.0", info("v1.0.0"), true, "2.0.0"},
		{"module version without its v", "dev", info("v1.2.3"), true, "1.2.3"},
		{"devel is dev", "dev", info("(devel)"), true, "dev"},
		{"empty module version is dev", "dev", info(""), true, "dev"},
		{"a bare v is dev", "dev", info("v"), true, "dev"},
		{"no build info is dev", "dev", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.ldflags, c.info, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLoginCommandsKeepStdoutEmpty(t *testing.T) {
	for _, args := range [][]string{{"login", "nope"}, {"logout", "nope"}, {"surprise"}, {"login"}} {
		r := start(t, binary(t, ""), args, 0)
		if r.code != 2 || len(r.stdout) != 0 || !strings.Contains(r.stderr, "Brug: handle-mcp login") || !strings.Contains(r.stderr, "nemlig") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, r.code, r.stdout, r.stderr)
		}
	}
}
