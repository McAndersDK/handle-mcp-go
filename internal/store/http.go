package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Retries is the number of extra attempts after the first one.
const Retries = 2

// maxBody caps what one answer may read into memory.
const maxBody = 32 << 20

// Request is one HTTP call. Body is bytes, so each attempt builds a fresh reader.
type Request struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// Response is what the caller gets: the status, the final URL (after redirects the client
// followed), the headers and the whole body. The body is already closed.
type Response struct {
	Status int
	URL    string
	Header http.Header
	Body   []byte
}

// Sender sends requests with the retries every store needs. Retry only on timeout, network
// errors, 429 and 5xx. Safe because every write sets an absolute quantity (ADR 0005 keeps this).
// The caller configures Client (timeout, cookie jar, redirects).
type Sender struct {
	Client *http.Client
	Store  string // the name in error messages, e.g. "nemlig.com"
	// Sleep waits between attempts and returns early with ctx's error. nil = a real wait.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Send makes the request. Errors are StoreErrors with a fixed word for the kind of failure:
// never the Go error text, because a *url.Error holds the full URL with tokens and ids.
// A cancelled ctx returns ctx's error.
func (s *Sender) Send(ctx context.Context, r Request) (*Response, error) {
	for attempt := 0; attempt <= Retries; attempt++ {
		last := attempt == Retries
		resp, err := s.once(ctx, r)
		switch {
		case err == nil && resp.Status != http.StatusTooManyRequests && resp.Status < 500:
			return resp, nil
		case err == nil:
			if last {
				return nil, Errorf("%s svarer %d. Prøv igen senere.", s.Store, resp.Status)
			}
		case errors.Is(err, context.Canceled):
			return nil, context.Canceled // not err: a *url.Error holds the URL
		default:
			kind, retry := classify(err)
			if !retry {
				return nil, Errorf("Kan ikke nå %s (%s).", s.Store, kind)
			}
			if ctx.Err() != nil || last {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil, ctx.Err()
				}
				return nil, Errorf("%s svarer ikke (%s).", s.Store, kind)
			}
		}
		if err := s.sleep(ctx, 500*time.Millisecond<<attempt); err != nil {
			return nil, err
		}
	}
	panic("unreachable")
}

func (s *Sender) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Sender) once(ctx context.Context, r Request) (*Response, error) {
	var body io.Reader
	if r.Body != nil {
		body = bytes.NewReader(r.Body) // new for every attempt
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, body)
	if err != nil {
		return nil, errBadRequest
	}
	for k, v := range r.Header {
		req.Header[k] = append([]string(nil), v...)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	final := r.URL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &Response{Status: resp.StatusCode, URL: final, Header: resp.Header, Body: data}, nil
}

var errBadRequest = errors.New("bad request")

// classify turns a transport error into a fixed word and says whether a retry is safe.
func classify(err error) (kind string, retry bool) {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // every *url.Error is a net.Error; look at what is inside
	}
	var ne net.Error
	switch {
	case errors.Is(err, errBadRequest):
		return "request", false
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout", true
	case errors.As(err, &ne), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "network", true
	}
	return "other", false
}
