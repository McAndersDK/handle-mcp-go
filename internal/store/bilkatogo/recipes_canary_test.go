package bilkatogo

// ADR 0005, privacy test 2, for the recipe methods. canary_test.go deep-merges the LEAK map into
// every JSON answer. A payload.js is not JSON, so here it is read with the real reader, the leak
// is merged into every object of the tree, and the page is written back as a JavaScript literal.
// The results go through privacy.Out and no LEAK value may be in the output.

import (
	"bytes"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/privacy"
)

// leakNode turns a value of leak() into a payload tree.
func leakNode(v any) any {
	m, ok := v.(obj)
	if !ok {
		return v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	o := &pobj{}
	for _, k := range keys {
		o.set(k, leakNode(m[k]))
	}
	return o
}

// mergeLeakNode puts the leak into every object of the tree. A key that is already there stays;
// two objects are merged one level down.
func mergeLeakNode(v any) {
	switch x := v.(type) {
	case *pobj:
		for _, k := range x.keys {
			mergeLeakNode(x.vals[k])
		}
		for k, sv := range leak() {
			dv, there := x.vals[k]
			dst, dstObj := dv.(*pobj)
			src, _ := leakNode(sv).(*pobj)
			switch {
			case !there:
				x.set(k, leakNode(sv))
			case dstObj && src != nil:
				for _, sk := range src.keys {
					if _, has := dst.vals[sk]; !has {
						dst.set(sk, src.vals[sk])
					}
				}
			}
		}
	case []any:
		for _, e := range x {
			mergeLeakNode(e)
		}
	}
}

// leakyPages puts the leak into every payload.js the website sends.
type leakyPages struct{ inner http.RoundTripper }

func (l leakyPages) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.inner.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/payload.js") {
		return resp, err
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if top, perr := parsePayload(string(raw)); perr == nil && bytes.HasPrefix(raw, []byte("__NUXT_JSONP__")) {
		mergeLeakNode(top)
		raw = []byte(`__NUXT_JSONP__("/x", ` + payloadJSON(top) + `);`)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	return resp, nil
}

func TestCanaryMergeIntoAPayloadKeepsRealValues(t *testing.T) {
	top, err := parsePayload(`{Email:"kunde@example.com",deliveryAddress:{dawa_uid:"x"},inner:[{DoorCode:null}]}`)
	if err != nil {
		t.Fatal(err)
	}
	mergeLeakNode(top)
	addr := top.obj("deliveryAddress")
	inner := asNode(top.list("inner")[0])
	if top.text("Email") != "kunde@example.com" || addr.text("dawa_uid") != "x" || addr.text("city") != "Andeby" {
		t.Errorf("real values or the merge are wrong: %s", payloadJSON(top))
	}
	if v, there := inner.vals["DoorCode"]; !there || v != nil || inner.text("CustomerName") != "Anders And" {
		t.Errorf("the leak is not in every object: %s", payloadJSON(inner))
	}
}

func TestCanaryNothingLeaksThroughTheRecipeMethods(t *testing.T) {
	h := envLogin(t)
	w := newSiteFake(h.f)
	h.s = New(&http.Client{Transport: leaky{leakyPages{w}}}, WithClock(func() time.Time { return h.clock }))
	scriptHits(h, karbonaderHits(t))
	// The canary is really in the pages the store reads.
	page, err := h.s.page(bg, featured)
	if err != nil || page.text("CustomerName") != "Anders And" {
		t.Fatalf("the canary is not in the pages (%v)", err)
	}
	if nodes(page, "Heading")[0].text("Email") != "anders@example.com" {
		t.Fatal("the canary is not in the nested objects")
	}
	dir, sub := "aftensmad/danske-retter", "aftensmad"
	reads := map[string]func() (any, error){
		"featured":    func() (any, error) { return h.s.FeaturedRecipes(bg, 7) },
		"category":    func() (any, error) { return h.s.RecipesByTag(bg, nil, &dir, 20) },
		"sub-folders": func() (any, error) { return h.s.RecipesByTag(bg, nil, &sub, 20) },
		"recipe":      func() (any, error) { return h.s.Recipe(bg, karbonader, nil) },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			result, err := read()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out, err := privacy.Out(result)
			if err != nil || len(out) < 20 {
				t.Fatalf("%s: no answer (%v)", name, err)
			}
			for _, secret := range append(leakValues(), fixtureValues()...) {
				if strings.Contains(out, secret) {
					t.Errorf("%s leaks %q:\n%s", name, secret, out)
				}
			}
		})
	}
}
