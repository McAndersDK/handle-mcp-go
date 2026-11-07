package bilkatogo

// Replays every golden case in tests/golden/stores/bilkatogo through the real adapter, with the
// fake BilkaToGo of fake_test.go as the only network. It checks the answer or the error, every
// request (method, URL, params, body, chosen headers, cookie names), the keychain and the session.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/McAndersDK/handle-mcp-go/internal/goldentest"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("HANDLE_LIVE") != "1" {
		keyring.MockInit() // no test may reach the real keychain; every test also calls isolate
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// wantCases is the number of golden files. A golden that shrinks or grows fails, and so does a
// case this test did not read.
const wantCases = 117

// Named exceptions: where Go deliberately differs from the recorded Python. Each has a count.
const (
	// wantDSTGapCases: a slot time that does not exist on the wall clock (02:xx on 2026-03-29) is
	// shown as the same moment in the new offset (03:xx+02:00), not as Python's 02:xx+01:00.
	wantDSTGapCases = 1
	wantDSTGapTimes = 4
	// wantSenderWordCases: Python names the httpx exception class in the error text, Go says a
	// fixed word (store.Sender).
	wantSenderWordCases = 2
	// There are no missing-price cases in the BilkaToGo goldens: the rule (ADR 0005) is tested by
	// hand in hand_test.go.
)

var senderWords = map[string]string{
	"BilkaToGo svarer ikke (ConnectTimeout).":      "BilkaToGo svarer ikke (timeout).",
	"Kan ikke nå BilkaToGo (UnsupportedProtocol).": "Kan ikke nå BilkaToGo (other).",
}

type goldenReq struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Params  [][]string        `json:"params"`
	Body    any               `json:"body"`
	Headers map[string]string `json:"headers"`
	Cookies []string          `json:"cookies"`
}

type goldenErr struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type goldenStep struct {
	Method   string          `json:"method"`
	Args     []any           `json:"args"`
	Answer   json.RawMessage `json:"answer"`
	Error    *goldenErr      `json:"error"`
	Requests []goldenReq     `json:"requests"`
}

type goldenCase struct {
	goldenStep
	Name           string            `json:"name"`
	Login          *string           `json:"login"`
	Env            map[string]string `json:"env"`
	KeychainBefore any               `json:"keychain_before"`
	KeychainAfter  any               `json:"keychain_after"`
	SessionAfter   any               `json:"session_after"`
	Steps          []goldenStep      `json:"steps"`
}

// exceptions counts the named exceptions that really happened.
type exceptions struct {
	dst, sender, pickup map[string]bool
	dstValues           int // the times in those cases that differ
}

func setSecret(tb testing.TB, text string) {
	tb.Helper()
	if err := keyring.Set(store.KeychainService, name, text); err != nil {
		tb.Fatal(err)
	}
}

// isolate gives a case a clean world: no HANDLE_ variable, an empty config folder, an empty
// keychain.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "HANDLE_") {
			old := os.Getenv(key)
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, old) })
		}
	}
	t.Setenv("HANDLE_CONFIG_DIR", t.TempDir())
}

// pinned is the clock of the goldens: 2026-11-02T12:00:00+01:00.
var pinned = time.Date(2026, 11, 2, 12, 0, 0, 0, time.FixedZone("", 3600))

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{tb: t, f: newFake(t), clock: pinned}
	h.s = New(&http.Client{Transport: h.f},
		WithClock(func() time.Time { return h.clock }),
		WithSleep(func(context.Context, time.Duration) error { return nil }))
	return h
}

func TestGoldenBilkatogo(t *testing.T) {
	files, err := filepath.Glob(goldentest.Path("stores/bilkatogo/*.json"))
	if err != nil || len(files) != wantCases {
		t.Fatalf("%d golden files, want %d (err %v)", len(files), wantCases, err)
	}
	cases := table()
	if len(cases) != wantCases {
		t.Fatalf("the case table has %d entries, want %d", len(cases), wantCases)
	}
	ex := &exceptions{dst: map[string]bool{}, sender: map[string]bool{}, pickup: map[string]bool{}}
	ran := 0
	for _, file := range files {
		rel := "stores/bilkatogo/" + filepath.Base(file)
		var g goldenCase
		goldentest.Read(t, rel, &g)
		hooks, ok := cases[g.Name]
		if !ok || g.Name+".json" != filepath.Base(file) {
			t.Fatalf("%s: no setup in the case table", rel)
		}
		delete(cases, g.Name)
		ran++
		t.Run(g.Name, func(t *testing.T) { runCase(t, &g, hooks, ex) })
	}
	if ran != wantCases || len(cases) != 0 {
		t.Fatalf("ran %d cases, %d table entries unused", ran, len(cases))
	}
	if len(ex.dst) != wantDSTGapCases || ex.dstValues != wantDSTGapTimes || len(ex.sender) != wantSenderWordCases || len(ex.pickup) != wantPickupCases {
		t.Errorf("named exceptions: dst gap %v (%d times), sender words %v, pickup %v",
			keys(ex.dst), ex.dstValues, keys(ex.sender), keys(ex.pickup))
	}
	t.Logf("golden cases run: %d, skipped: 0; named exceptions: dst gap %d case(s) / %d time(s), sender words %d, pickup %d, missing price 0",
		ran, len(ex.dst), ex.dstValues, len(ex.sender), len(ex.pickup))
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func runCase(t *testing.T, g *goldenCase, hooks []hook, ex *exceptions) {
	isolate(t)
	if g.KeychainBefore != nil {
		raw, _ := json.Marshal(g.KeychainBefore)
		setSecret(t, string(raw))
	}
	for k, v := range g.Env {
		t.Setenv(k, v)
	}
	h := newHarness(t)
	steps := g.Steps
	if steps == nil {
		steps = []goldenStep{g.goldenStep}
	}
	seen := 0
	for i, step := range steps {
		if i < len(hooks) && hooks[i] != nil {
			hooks[i](h)
		}
		answer, err := invoke(t, h.s, step)
		h.f.mu.Lock()
		got := slices.Clone(h.f.requests[seen:])
		seen = len(h.f.requests)
		h.f.mu.Unlock()
		compareRequests(t, step, got)
		compareOutcome(t, g.Name, step, answer, err, ex)
	}
	if kc := readKeychain(t); !equalJSON(g.KeychainAfter, kc) {
		t.Errorf("keychain after: got %v, want %v", kc, g.KeychainAfter)
	}
	var session any
	if sess := h.s.Session(); sess != nil {
		session = toPlain(t, sess)
	}
	if !equalJSON(g.SessionAfter, session) {
		t.Errorf("session after: got %v, want %v", session, g.SessionAfter)
	}
}

func readKeychain(t *testing.T) any {
	t.Helper()
	secret, err := keyring.Get(store.KeychainService, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	v, err := decodeJSON([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// toPlain marshals a value and reads it back as plain JSON values.
func toPlain(t testing.TB, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalJSON(a, b any) bool { return reflect.DeepEqual(normalise(a), normalise(b)) }

// normalise makes json.Number("1") and json.Number("1.0") the same when they are the same number.
func normalise(v any) any {
	switch x := v.(type) {
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalise(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalise(e)
		}
		return out
	}
	return v
}

// invoke calls the store method a golden step names. The golden JSON sorts object keys, so the
// order of set_quantities items comes from the ChangeLineCount requests it recorded.
func invoke(t *testing.T, s *Store, step goldenStep) (any, error) {
	t.Helper()
	ctx := context.Background()
	arg := func(i int, def any) any {
		if i < len(step.Args) {
			return step.Args[i]
		}
		return def
	}
	str := func(i int) string { return arg(i, "").(string) }
	num := func(i, def int) int {
		if i >= len(step.Args) {
			return def
		}
		n, _ := step.Args[i].(json.Number).Int64()
		return int(n)
	}
	switch step.Method {
	case "search":
		return s.Search(ctx, str(0), num(1, 10))
	case "offers":
		return s.Offers(ctx, num(0, 25))
	case "recipes":
		return s.Recipes(ctx, str(0), num(1, 5))
	case "recipe":
		var persons *int
		if p, ok := arg(1, nil).(json.Number); ok {
			n, _ := p.Int64()
			v := int(n)
			persons = &v
		}
		return s.Recipe(ctx, str(0), persons)
	case "featured_recipes":
		return s.FeaturedRecipes(ctx, num(0, 20))
	case "recipes_by_tag":
		var tags []string
		for _, tg := range arg(0, []any{}).([]any) {
			tags = append(tags, tg.(string))
		}
		var category *string
		if c, ok := arg(1, nil).(string); ok {
			category = &c
		}
		return s.RecipesByTag(ctx, tags, category, num(2, 20))
	case "get_cart":
		return s.GetCart(ctx)
	case "terms":
		return s.Terms(ctx)
	case "delivery_info":
		return s.DeliveryInfo(ctx)
	case "delivery_slots":
		return s.DeliverySlots(ctx, num(0, 7))
	case "orders":
		return s.Orders(ctx, num(0, 10))
	case "order":
		return s.Order(ctx, str(0))
	case "session":
		if sess := s.Session(); sess != nil {
			return sess, nil
		}
		return nil, nil
	case "set_quantities":
		return s.SetQuantities(ctx, itemsInRequestOrder(t, arg(0, obj{}).(obj), step.Requests))
	}
	t.Fatalf("unknown method %q", step.Method)
	return nil, nil
}

func itemsInRequestOrder(t *testing.T, items obj, reqs []goldenReq) []store.CartItem {
	var order []string
	for _, r := range reqs {
		if strings.HasSuffix(r.URL, "/ChangeLineCount") {
			for _, p := range r.Params {
				if p[0] == "productId" && !slices.Contains(order, p[1]) {
					order = append(order, p[1])
				}
			}
		}
	}
	var rest []string
	for id := range items {
		if !slices.Contains(order, id) {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	var out []store.CartItem
	for _, id := range append(order, rest...) {
		n, err := items[id].(json.Number).Int64()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, store.CartItem{ProductID: id, Quantity: int(n)})
	}
	return out
}

func compareOutcome(t *testing.T, caseName string, step goldenStep, answer any, err error, ex *exceptions) {
	t.Helper()
	if checkPickupException(t, caseName, step, answer, err, ex) {
		return // a named pickup exception (docs/adr/0006), compared with the new keys
	}
	if step.Error != nil {
		if step.Error.Type == "NotImplementedError" {
			if !errors.Is(err, store.ErrNotImplemented) {
				t.Errorf("want ErrNotImplemented, got %v", err)
			}
			return
		}
		var se *store.StoreError
		if !errors.As(err, &se) {
			t.Errorf("want StoreError %q, got %#v", step.Error.Message, err)
			return
		}
		want := step.Error.Message
		if word, ok := senderWords[want]; ok {
			want = word
			ex.sender[caseName] = true
		}
		if se.Msg != want {
			t.Errorf("error: got %q, want %q", se.Msg, want)
		}
		return
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	want, derr := decodeJSON(step.Answer)
	if derr != nil {
		t.Fatalf("golden answer: %v", derr)
	}
	if !equalAnswer(want, goldentest.DropAdded(toPlain(t, answer)), caseName, ex) {
		t.Errorf("answer differs:\n got  %s\n want %s", jsonText(toPlain(t, answer)), jsonText(want))
	}
}

func jsonText(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(buf.String())
}

// equalAnswer is a deep compare, with one named exception: a time that does not exist on the
// Danish wall clock.
func equalAnswer(want, got any, caseName string, ex *exceptions) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, v := range w {
			if gv, ok := g[k]; !ok || !equalAnswer(v, gv, caseName, ex) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !equalAnswer(w[i], g[i], caseName, ex) {
				return false
			}
		}
		return true
	case string:
		g, ok := got.(string)
		if !ok {
			return false
		}
		if g == w {
			return true
		}
		if sameMomentInGap(w, g) {
			ex.dst[caseName] = true
			ex.dstValues++
			return true
		}
		return false
	}
	return equalJSON(want, got)
}

// sameMomentInGap: Python wrote a wall-clock time that never happened (02:xx+01:00 on the day the
// clocks go forward); Go writes the same moment as 03:xx+02:00.
func sameMomentInGap(want, got string) bool {
	w, err1 := time.Parse(time.RFC3339, want)
	g, err2 := time.Parse(time.RFC3339, got)
	if err1 != nil || err2 != nil || !w.Equal(g) {
		return false
	}
	y, m, d := w.Date()
	local := time.Date(y, m, d, w.Hour(), w.Minute(), w.Second(), 0, dk)
	return local.Hour() != w.Hour() // the wall time does not exist in Copenhagen
}

// compareRequests checks the requests a step made against the recorded ones.
func compareRequests(t *testing.T, step goldenStep, got []record) {
	t.Helper()
	want := slices.Clone(step.Requests)
	if step.Method == "delivery_slots" {
		// The slots and the cart are asked for at once; after the login their order is not fixed.
		sortAfterLogin(want, func(r goldenReq) string { return r.URL })
		sortAfterLogin(got, func(r record) string { return r.URL })
	}
	if len(got) != len(want) {
		t.Errorf("%d requests, want %d:\n got  %v\n want %v", len(got), len(want), urls(got), wantURLs(want))
		return
	}
	for i, w := range want {
		g := got[i]
		if g.Method != w.Method || g.URL != w.URL {
			t.Errorf("request %d: %s %s, want %s %s", i, g.Method, g.URL, w.Method, w.URL)
			continue
		}
		var params [][]string
		for _, p := range g.Params {
			params = append(params, []string{p[0], p[1]})
		}
		if !reflect.DeepEqual(nonNil(params), nonNil(w.Params)) {
			t.Errorf("request %d (%s): params %v, want %v", i, w.URL, params, w.Params)
		}
		if !equalJSON(g.Body, w.Body) {
			t.Errorf("request %d (%s): body %v, want %v", i, w.URL, g.Body, w.Body)
		}
		if !reflect.DeepEqual(g.Headers, w.Headers) {
			t.Errorf("request %d (%s): headers %v, want %v", i, w.URL, g.Headers, w.Headers)
		}
		if !slices.Equal(g.Cookies, nonNil(w.Cookies)) {
			t.Errorf("request %d (%s): cookies %v, want %v", i, w.URL, g.Cookies, w.Cookies)
		}
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// sortAfterLogin orders the requests that follow LoginJWT by URL, keeping the order of the ones
// to the same URL: AvailableDeliveryDates (v3) before Cart (v6).
func sortAfterLogin[T any](rs []T, url func(T) string) {
	from := 0
	for i, r := range rs {
		if strings.HasSuffix(url(r), "/auth/LoginJWT") {
			from = i + 1
		}
	}
	tail := rs[from:]
	sort.SliceStable(tail, func(a, b int) bool { return url(tail[a]) < url(tail[b]) })
}

func urls(rs []record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Method+" "+r.URL)
	}
	return out
}

func wantURLs(rs []goldenReq) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Method+" "+r.URL)
	}
	return out
}
