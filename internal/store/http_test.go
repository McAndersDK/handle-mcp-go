package store

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit() // no test may reach the real keychain
	m.Run()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// answers plays the answers in order; the last one repeats. An int is a status, an error fails.
func answers(items ...any) (*Sender, *[]string, *[]time.Duration) {
	var bodies []string
	var waits []time.Duration
	n := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b := ""
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			b = string(data)
		}
		bodies = append(bodies, b)
		item := items[min(n, len(items)-1)]
		n++
		if err, ok := item.(error); ok {
			return nil, err
		}
		return &http.Response{StatusCode: item.(int), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header: http.Header{}, Request: r}, nil
	})
	s := &Sender{
		Client: &http.Client{Transport: rt},
		Store:  "Butik",
		Sleep:  func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}
	return s, &bodies, &waits
}

var get = Request{Method: "GET", URL: "https://x.test/a"}

func TestRetries5xxAnd429WithBackoff(t *testing.T) { // test_retries_5xx_and_429_with_backoff
	s, calls, waits := answers(503, 429, 200)
	resp, err := s.Send(context.Background(), get)
	if err != nil || resp.Status != 200 || string(resp.Body) != `{"ok":true}` || resp.URL != get.URL {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if len(*calls) != 3 || len(*waits) != 2 || (*waits)[0] != 500*time.Millisecond || (*waits)[1] != time.Second {
		t.Fatalf("calls=%d waits=%v", len(*calls), *waits)
	}
}

func TestGivesUpAfterRetries(t *testing.T) { // test_gives_up_after_retries
	s, calls, _ := answers(503)
	_, err := s.Send(context.Background(), get)
	if err == nil || err.Error() != "Butik svarer 503. Prøv igen senere." {
		t.Fatalf("err=%v", err)
	}
	if len(*calls) != Retries+1 {
		t.Fatalf("calls=%d", len(*calls))
	}
}

func Test4xxIsNotRetried(t *testing.T) { // test_4xx_is_not_retried
	s, calls, _ := answers(404)
	resp, err := s.Send(context.Background(), get)
	if err != nil || resp.Status != 404 || len(*calls) != 1 {
		t.Fatalf("resp=%+v err=%v calls=%d", resp, err, len(*calls))
	}
}

func TestTimeoutAndNetworkErrorsAreRetriedThenClear(t *testing.T) { // test_timeout_and_network_errors_...
	s, calls, _ := answers(timeoutErr{}, io.ErrUnexpectedEOF, 200)
	resp, err := s.Send(context.Background(), get)
	if err != nil || resp.Status != 200 || len(*calls) != 3 {
		t.Fatalf("resp=%+v err=%v calls=%d", resp, err, len(*calls))
	}

	s, calls, _ = answers(&net.OpError{Op: "dial", Err: errors.New("refused")})
	_, err = s.Send(context.Background(), get)
	if err == nil || err.Error() != "Butik svarer ikke (network)." || len(*calls) != Retries+1 {
		t.Fatalf("err=%v calls=%d", err, len(*calls))
	}

	s, _, _ = answers(timeoutErr{})
	if _, err = s.Send(context.Background(), get); err == nil || err.Error() != "Butik svarer ikke (timeout)." {
		t.Fatalf("err=%v", err)
	}
}

func TestOtherErrorsAreNotRetried(t *testing.T) { // test_other_http_errors_are_not_retried
	s, calls, _ := answers(errors.New("stopped after 10 redirects"))
	_, err := s.Send(context.Background(), get)
	if err == nil || err.Error() != "Kan ikke nå Butik (other)." || len(*calls) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(*calls))
	}
}

func TestBodyIsBuiltAnewForEveryAttempt(t *testing.T) {
	s, calls, _ := answers(503, 503, 200)
	r := Request{Method: "POST", URL: "https://x.test/a", Body: []byte(`{"q":1}`)}
	if _, err := s.Send(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for i, b := range *calls {
		if b != `{"q":1}` {
			t.Fatalf("attempt %d sent %q", i, b)
		}
	}
}

func TestErrorsNeverHoldTheURL(t *testing.T) {
	const secret = "tok-SECRET-9"
	for _, failure := range []error{timeoutErr{}, io.EOF, errors.New("boom " + secret)} {
		s, _, _ := answers(failure)
		_, err := s.Send(context.Background(), Request{Method: "GET", URL: "https://x.test/a?token=" + secret})
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "x.test") {
			t.Fatalf("leaky error: %v", err)
		}
		var unwrapped *StoreError
		if !errors.As(err, &unwrapped) || errors.Unwrap(err) != nil {
			t.Fatalf("the error must be a bare StoreError, got %T", err)
		}
	}
	s, _, _ := answers(200)
	_, err := s.Send(context.Background(), Request{Method: "GET", URL: "https://x.test/%zz?token=" + secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("bad URL leaked: %v", err)
	}
}

func TestSleepRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, calls, _ := answers(503)
	s.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	_, err := s.Send(ctx, get)
	if !errors.Is(err, context.Canceled) || len(*calls) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(*calls))
	}
	// The real wait also stops at once.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	s.Sleep = nil
	if err := s.sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelledContextStopsAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, calls, _ := answers(200)
	s.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	_, err := s.Send(ctx, get)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	_ = calls
}

func TestNoRetrySendsOnceAndReturnsEveryStatus(t *testing.T) {
	for _, status := range []int{500, 503, 429} {
		s, bodies, waits := answers(status, 200)
		resp, err := s.Send(context.Background(), Request{Method: "POST", URL: "https://x.test/a", NoRetry: true})
		if err != nil || resp.Status != status || len(*bodies) != 1 || len(*waits) != 0 {
			t.Errorf("status %d: resp %v err %v attempts %d", status, resp, err, len(*bodies))
		}
	}
}

func TestNoRetryMarksATransportFailureUnsure(t *testing.T) {
	s, bodies, _ := answers(timeoutErr{}, 200)
	_, err := s.Send(context.Background(), Request{Method: "POST", URL: "https://x.test/a", NoRetry: true})
	var se *StoreError
	if !errors.As(err, &se) || !se.Unsure || len(*bodies) != 1 {
		t.Fatalf("err %v, attempts %d", err, len(*bodies))
	}
	s, bodies, _ = answers(timeoutErr{}, 200)
	if _, err := s.Send(context.Background(), get); err != nil || len(*bodies) != 2 {
		t.Errorf("a normal request must still retry: err %v attempts %d", err, len(*bodies))
	}
}
