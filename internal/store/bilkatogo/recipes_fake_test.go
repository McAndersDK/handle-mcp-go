package bilkatogo

// The website part of the fake: www.bilkatogo.dk, as it answered on 2026-11-06 (the fixtures in
// tests/fixtures/bilkatogo/recipes). Everything else goes to the fake of fake_test.go.

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const recipeBuild = "1793920097" // the build id inside the fixtures

// recipePages: payload path → fixture (a page the site has).
var recipePages = map[string]string{
	"":                                   "opskrifter",
	"madplaner":                          "madplaner",
	"aftensmad":                          "aftensmad",
	"aftensmad/danske-retter":            "danske-retter",
	"aftensmad/danske-retter/karbonader": "karbonader",
	"aftensmad/pastaretter/carbonara":    "carbonara",
}

const karbonader = "https://www.bilkatogo.dk/inspiration/opskrifter/aftensmad/danske-retter/karbonader/"

func recipeFixturePath(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "tests", "fixtures", "bilkatogo", "recipes", name)
}

var recipeTexts sync.Map // fixture name → its text: read once, used by many goroutines

// recipeText is a fixture file as text. A missing file stops the test run.
func recipeText(name string) string {
	if v, ok := recipeTexts.Load(name); ok {
		return v.(string)
	}
	raw, err := os.ReadFile(recipeFixturePath(name))
	if err != nil {
		panic(err)
	}
	recipeTexts.Store(name, string(raw))
	return string(raw)
}

// siteFake is bilkatogo.dk: HTML with the build id, and each page's payload.js. A page it does not
// know answers like the real site: HTTP 200 with the website's HTML.
type siteFake struct {
	inner http.RoundTripper // Gigya, api.bilkatogo.dk and Algolia

	mu       sync.Mutex
	build    string
	paths    []string          // every path asked on the website, in order
	override map[string]string // page path → a payload of the test's own
	delay    time.Duration     // a payload answers after this (to see pages read at once)
	now, max int               // payloads being answered now, and the most at one time
}

func newSiteFake(inner http.RoundTripper) *siteFake {
	return &siteFake{inner: inner, build: recipeBuild, override: map[string]string{}}
}

func (w *siteFake) setBuild(b string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.build = b
}

func (w *siteFake) count(path string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, p := range w.paths {
		if p == path {
			n++
		}
	}
	return n
}

func (w *siteFake) saw(suffix string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.paths {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

func (w *siteFake) maxAtOnce() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.max
}

func reply(req *http.Request, text string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200", Header: http.Header{}, Request: req,
		Body: io.NopCloser(bytes.NewReader([]byte(text))), ContentLength: int64(len(text))}
}

func (w *siteFake) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil { // a real transport stops for a cancelled call
		return nil, err
	}
	if req.URL.Host != "www.bilkatogo.dk" {
		return w.inner.RoundTrip(req)
	}
	w.mu.Lock()
	path, build, delay := req.URL.Path, w.build, w.delay
	w.paths = append(w.paths, path)
	prefix := "/_nuxt/static/" + build + "/inspiration/opskrifter"
	isPayload := strings.HasPrefix(path, "/_nuxt/static/"+build+"/") && strings.HasSuffix(path, "/payload.js")
	page := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/payload.js"), "/")
	body, own := w.override[page]
	if isPayload {
		w.now++
		w.max = max(w.max, w.now)
	}
	w.mu.Unlock()
	if isPayload {
		defer func() { w.mu.Lock(); w.now--; w.mu.Unlock() }()
		time.Sleep(delay)
		if own {
			return reply(req, body), nil
		}
		name, ok := recipePages[page]
		if !ok && strings.HasPrefix(page, "aftensmad/") { // every other page: one of each
			name, ok = "danske-retter", true
			if strings.Count(page, "/") == 2 {
				name = "karbonader"
			}
		}
		if ok {
			return reply(req, recipeText(name+".payload.js")), nil
		}
	}
	html := strings.ReplaceAll(recipeText("unknown-page.html"), recipeBuild, build)
	return reply(req, html), nil
}
