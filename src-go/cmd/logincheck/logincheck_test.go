package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

var secrets = []string{"hemmeligt-4711", "a@example.com", "tok-123", "sess-secret", "xsrf-9", "jwt-xyz"}

type fakeRT struct {
	mu      sync.Mutex
	handler func(*http.Request, int) *http.Response
	calls   []string
	cookies []string // the Cookie header of every call
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req.Method+" "+req.URL.Host+req.URL.Path)
	f.cookies = append(f.cookies, req.Header.Get("Cookie"))
	n := len(f.calls)
	f.mu.Unlock()
	resp := f.handler(req, n)
	resp.Request = req
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	return resp, nil
}

func reply(status int, body string, setCookies ...string) *http.Response {
	h := http.Header{}
	for _, c := range setCookies {
		h.Add("Set-Cookie", c)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func newApp(rt http.RoundTripper) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errw bytes.Buffer
	a := &app{
		out: &out, errw: &errw, inner: rt,
		in:     bufio.NewReader(strings.NewReader("a@example.com\n\n")),
		secret: func() (string, error) { return "hemmeligt-4711", nil },
	}
	return a, &out, &errw
}

func noSecrets(t *testing.T, outs ...*bytes.Buffer) {
	t.Helper()
	for _, o := range outs {
		for _, s := range secrets {
			if strings.Contains(o.String(), s) {
				t.Errorf("output contains a secret: %q", s)
			}
		}
	}
}

func req(method, url string) *http.Request {
	r, _ := http.NewRequest(method, url, nil)
	return r
}

func TestGuardRefuses(t *testing.T) {
	inner := &fakeRT{handler: func(*http.Request, int) *http.Response { return reply(200, "{}") }}
	g := guard{inner}
	for _, r := range []*http.Request{
		req("POST", "https://www.nemlig.com/webapi/basket/AddToBasket"),
		req("GET", "https://www.nemlig.com/webapi/unknown"),
		req("GET", "https://evil.example.com/webapi/Token"),
		req("POST", "https://www.nemlig.com/webapi/Token"), // wrong method
		req("GET", "http://www.nemlig.com/webapi/Token"),   // not https
		req("GET", "https://api.bilkatogo.dk/api/shop/v6/ChangeLineCount"),
	} {
		_, err := g.RoundTrip(r)
		var b *blockedError
		if !errors.As(err, &b) {
			t.Errorf("%s %s was not refused", r.Method, r.URL)
		}
	}
	if len(inner.calls) != 0 {
		t.Errorf("refused requests reached the transport: %v", inner.calls)
	}
}

func TestGuardCartCookie(t *testing.T) {
	inner := &fakeRT{handler: func(*http.Request, int) *http.Response { return reply(200, "{}") }}
	r := req("POST", "https://api.bilkatogo.dk/api/auth/LoginJWT?u=w")
	r.AddCookie(&http.Cookie{Name: "cart", Value: "x"})
	if _, err := (guard{inner}).RoundTrip(r); err == nil || len(inner.calls) != 0 {
		t.Fatal("LoginJWT with a cart cookie was not refused")
	}
	r = req("POST", "https://api.bilkatogo.dk/api/auth/LoginJWT?u=w")
	if _, err := (guard{inner}).RoundTrip(r); err != nil {
		t.Fatalf("LoginJWT without cookie refused: %v", err)
	}
}

func runNemlig(t *testing.T, h func(*http.Request, int) *http.Response) (int, *bytes.Buffer, *bytes.Buffer, *fakeRT) {
	rt := &fakeRT{handler: h}
	a, out, errw := newApp(rt)
	code := a.run([]string{"nemlig"})
	noSecrets(t, out, errw)
	return code, out, errw, rt
}

func TestQueueItRedirectBlocked(t *testing.T) {
	code, out, _, _ := runNemlig(t, func(*http.Request, int) *http.Response {
		r := reply(302, "")
		r.Header.Set("Location", "https://nemlig.queue-it.net/?c=x")
		return r
	})
	if code == 0 || !strings.Contains(out.String(), "BLOCKED pre_token") {
		t.Errorf("code=%d out=%q", code, out.String())
	}
}

func TestNonJSONBlocked(t *testing.T) {
	code, out, _, _ := runNemlig(t, func(*http.Request, int) *http.Response { return reply(200, "<html>hi</html>") })
	if code == 0 || !strings.Contains(out.String(), "BLOCKED pre_token") {
		t.Errorf("code=%d out=%q", code, out.String())
	}
}

func TestNoRetryOn500(t *testing.T) {
	code, out, _, rt := runNemlig(t, func(*http.Request, int) *http.Response { return reply(500, "{}") })
	if code == 0 || len(rt.calls) != 1 || strings.Contains(out.String(), "BLOCKED") {
		t.Errorf("code=%d calls=%v out=%q", code, rt.calls, out.String())
	}
}

func TestNemligHappyPath(t *testing.T) {
	code, out, errw, rt := runNemlig(t, func(r *http.Request, _ int) *http.Response {
		switch r.URL.Path {
		case "/webapi/Token":
			return reply(200, `{"access_token":"tok-123"}`)
		case "/webapi/AntiForgery":
			return reply(200, `{"Value":"xsrf-9"}`)
		case "/webapi/login":
			return reply(200, `{"RedirectUrl":"/"}`, ".ASPXAUTH=sess-secret; Domain=.nemlig.com; Path=/")
		case "/webapi/basket/GetBasket":
			if strings.Contains(r.Header.Get("Cookie"), ".ASPXAUTH=sess-secret") {
				return reply(200, `{"Email":"a@example.com","DeliveryZoneId":3,"Lines":[{"Quantity":2},{"Quantity":0}]}`)
			}
			return reply(200, `{"Email":"","Lines":[]}`)
		case "/webapi/v2/AppSettings/Website":
			return reply(200, `{"CombinedProductsAndSitecoreTimestamp":"123"}`)
		case "/searchgateway/api/search":
			return reply(200, `{"Products":{"Products":[{},{}]}}`)
		}
		return reply(404, "{}")
	})
	for _, want := range []string{"email_present=true line_count=1", "search_hits=2", "resume_email_present=true",
		"final_line_count=1", "session_json_bytes=", "RESULT OK"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %q", want, out.String())
		}
	}
	if code != 0 {
		t.Errorf("code=%d err=%q", code, errw.String())
	}
	if len(rt.calls) != 11 {
		t.Errorf("calls=%d %v", len(rt.calls), rt.calls)
	}
}

func TestNemligResumeFailed(t *testing.T) {
	code, out, _, _ := runNemlig(t, func(r *http.Request, _ int) *http.Response {
		switch r.URL.Path {
		case "/webapi/Token":
			return reply(200, `{"access_token":"tok-123"}`)
		case "/webapi/AntiForgery":
			return reply(200, `{"Value":"xsrf-9"}`)
		case "/webapi/login":
			return reply(200, `{"RedirectUrl":"/"}`, ".ASPXAUTH=sess-secret; Domain=.nemlig.com; Path=/")
		case "/webapi/basket/GetBasket":
			return reply(200, `{"Email":"","Lines":[]}`) // a dead cookie gives an anonymous basket
		case "/webapi/v2/AppSettings/Website":
			return reply(200, `{}`)
		case "/searchgateway/api/search":
			return reply(200, `{"Products":{"Products":[]}}`)
		}
		return reply(404, "{}")
	})
	if code != 3 || !strings.Contains(out.String(), "RESUME_FAILED") {
		t.Errorf("code=%d out=%q", code, out.String())
	}
}

func TestBilkaHappyPathAndResumeHasNoCookies(t *testing.T) {
	jwtCalls := 0
	var resumeStart int
	rt := &fakeRT{}
	rt.handler = func(r *http.Request, n int) *http.Response {
		switch r.URL.Path {
		case "/accounts.login":
			return reply(200, `{"errorCode":0,"sessionInfo":{"cookieValue":"tok-123"}}`, "gig=1; Domain=accounts.eu1.gigya.com; Path=/")
		case "/accounts.getJWT":
			jwtCalls++
			if jwtCalls == 2 {
				resumeStart = n
			}
			return reply(200, `{"errorCode":0,"id_token":"jwt-xyz"}`)
		case "/api/auth/LoginJWT":
			return reply(200, `{}`, "sid=abc; Domain=api.bilkatogo.dk; Path=/")
		case "/api/shop/v6/Cart":
			return reply(200, `{"deliveryAddress":{"sid":"123","dawa_uid":"x"},"lines":[`+
				`{"headline":"Mejeri","lines":[{"orderlines":[{},{}]}]},`+
				`{"headline":"Emballage","lines":[{"orderlines":[{}]}]}]}`)
		case "/1/indexes/*/queries":
			return reply(200, `{"results":[{"hits":[{},{},{}]}]}`)
		}
		return reply(404, "{}")
	}
	a, out, errw := newApp(rt)
	code := a.run([]string{"bilkatogo"})
	noSecrets(t, out, errw)
	for _, want := range []string{"line_count=2 home_delivery=true", "search_hits=3", "session_json_bytes=", "final_line_count=2", "RESULT OK"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %q", want, out.String())
		}
	}
	if code != 0 {
		t.Errorf("code=%d", code)
	}
	if resumeStart == 0 {
		t.Fatal("no resume")
	}
	sawCookie := false
	for i, c := range rt.cookies {
		if i+1 < resumeStart && c != "" {
			sawCookie = true // the first run does use its jar
		}
		if i+1 >= resumeStart && i+1 <= resumeStart+1 && c != "" {
			t.Errorf("resume call %d carried cookies", i+1)
		}
	}
	if !sawCookie {
		t.Error("the first run never used its jar; the test proves nothing")
	}
}

func TestBilkaGigyaErrorClass(t *testing.T) {
	rt := &fakeRT{handler: func(*http.Request, int) *http.Response {
		return reply(403, `{"errorCode":403042,"errorMessage":"hemmeligt-4711 a@example.com"}`)
	}}
	a, out, errw := newApp(rt)
	if a.run([]string{"bilkatogo"}) == 0 || !strings.Contains(out.String(), "kind=bad_login") || strings.Contains(out.String(), "BLOCKED") {
		t.Errorf("out=%q", out.String())
	}
	noSecrets(t, out, errw)
}
