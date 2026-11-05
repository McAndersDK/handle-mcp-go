// logincheck: a throwaway check that logged-in calls work from Go against nemlig.com and
// BilkaToGo. Memory only, one attempt per call, read-only. Never prints personal values.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	modeJSON  = iota // a 2xx JSON answer
	modeGigya        // any status, JSON with errorCode
	modeRaw          // a 2xx answer, body not read
)

// stepErr is a stopped run. It never holds a value from the stores.
type stepErr struct {
	step, kind string
	blocked    bool
}

func (e *stepErr) Error() string { return e.step + ": " + e.kind }

func fail(step, kind string) error   { return &stepErr{step: step, kind: kind} }
func blocked(step, why string) error { return &stepErr{step: step, kind: why, blocked: true} }

type app struct {
	out, errw io.Writer
	in        *bufio.Reader
	inner     http.RoundTripper
	secret    func() (string, error)
}

func (a *app) say(format string, args ...any) { fmt.Fprintf(a.out, format+"\n", args...) }

func (a *app) newClient(follow bool, timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Transport: guard{a.inner}, Jar: jar, Timeout: timeout}
	if !follow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

// classify turns a transport error into a step error without using its text (it holds the URL).
func classify(step string, err error) error {
	var b *blockedError
	if errors.As(err, &b) {
		return blocked(step, b.reason)
	}
	var ne net.Error
	var ua x509.UnknownAuthorityError
	var ci x509.CertificateInvalidError
	var he x509.HostnameError
	var rh tls.RecordHeaderError
	var cv *tls.CertificateVerificationError
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return fail(step, "timeout")
	case errors.As(err, &ua), errors.As(err, &ci), errors.As(err, &he), errors.As(err, &rh), errors.As(err, &cv):
		return fail(step, "tls")
	}
	return fail(step, "network")
}

// do sends one request, once. It prints the step, status and final host.
func (a *app) do(c *http.Client, step string, req *http.Request, mode int) (int, any, error) {
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, classify(step, err)
	}
	defer resp.Body.Close()
	a.say("step=%s status=%d final_host=%s", step, resp.StatusCode, resp.Request.URL.Hostname())
	if queueIt(resp.Request.URL.Hostname()) {
		return 0, nil, blocked(step, "queue_it")
	}
	if loc, err := resp.Location(); err == nil && queueIt(loc.Hostname()) {
		return 0, nil, blocked(step, "queue_it")
	}
	if resp.StatusCode == 403 && mode != modeGigya {
		return 0, nil, blocked(step, "forbidden")
	}
	if mode != modeGigya && (resp.StatusCode < 200 || resp.StatusCode > 299) {
		return resp.StatusCode, nil, fail(step, fmt.Sprintf("http_%d", resp.StatusCode))
	}
	if mode == modeRaw {
		return resp.StatusCode, nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return resp.StatusCode, nil, classify(step, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var data any
	if dec.Decode(&data) != nil {
		return resp.StatusCode, nil, blocked(step, "non_json")
	}
	if m, ok := data.(map[string]any); mode == modeGigya && resp.StatusCode == 403 && (!ok || m["errorCode"] == nil) {
		return resp.StatusCode, nil, blocked(step, "forbidden")
	}
	return resp.StatusCode, data, nil
}

// --- small JSON helpers -----------------------------------------------------

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f != 0
	}
	return true
}

func num(v any) int64 {
	if n, ok := v.(json.Number); ok {
		f, _ := n.Float64()
		return int64(f)
	}
	return 0
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func obj(step string, v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail(step, "unexpected_shape")
	}
	return m, nil
}

func sessionBytes(email string, session map[string]string) int {
	b, _ := json.Marshal(map[string]any{"email": email, "session": session})
	return len(b)
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// --- run --------------------------------------------------------------------

func (a *app) run(args []string) int {
	if len(args) != 1 || (args[0] != "nemlig" && args[0] != "bilkatogo") {
		fmt.Fprintln(a.errw, "usage: logincheck nemlig|bilkatogo")
		return 2
	}
	fmt.Fprintln(a.errw, "BEFORE YOU CONTINUE: note the cart's line count on the store's website now,")
	fmt.Fprintln(a.errw, "and compare it after the run. Nothing documents what nemlig's DoMerge login flag does.")
	fmt.Fprint(a.errw, "Email: ")
	email, _ := a.in.ReadString('\n')
	email = strings.TrimSpace(email)
	fmt.Fprint(a.errw, "Password (hidden): ")
	pw, err := a.secret()
	if err != nil {
		fmt.Fprintln(a.errw, "could not read the password")
		return 2
	}
	fmt.Fprint(a.errw, "Press Enter to start (no network call before this): ")
	_, _ = a.in.ReadString('\n')

	if args[0] == "nemlig" {
		err = a.nemlig(email, pw)
	} else {
		err = a.bilka(email, pw)
	}
	var se *stepErr
	switch {
	case err == nil:
		a.say("RESULT OK")
		return 0
	case errors.As(err, &se) && se.blocked:
		a.say("BLOCKED %s", se.step)
		fmt.Fprintf(a.errw, "blocked: %s\n", se.kind)
		return 2
	case errors.As(err, &se) && se.kind == "resume_failed":
		a.say("RESUME_FAILED")
		return 3
	case errors.As(err, &se):
		a.say("RESULT FAIL step=%s kind=%s", se.step, se.kind)
		return 1
	}
	a.say("RESULT FAIL")
	return 1
}

func realTransport() http.RoundTripper {
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DisableKeepAlives:   true, // no silent retry on a reused connection
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
}

func main() {
	a := &app{
		out: os.Stdout, errw: os.Stderr, in: bufio.NewReader(os.Stdin), inner: realTransport(),
		secret: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		},
	}
	os.Exit(a.run(os.Args[1:]))
}
