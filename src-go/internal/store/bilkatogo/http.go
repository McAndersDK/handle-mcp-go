package bilkatogo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

var jsonZero = json.Number("0")

// pairs are query or form values in the order they are sent.
type pairs [][2]string

// encode is urlencode(): spaces are "+", everything but letters, digits and "-_.~" is %XX.
func (p pairs) encode() string {
	parts := make([]string, len(p))
	for i, kv := range p {
		parts[i] = url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1])
	}
	return strings.Join(parts, "&")
}

// call is one request. Never log it: the URL and the form can hold a token, an id or an email.
type call struct {
	method string
	url    string
	query  pairs
	header http.Header
	form   pairs // sent as application/x-www-form-urlencoded
	json   any   // sent as JSON
}

// send is one request with retries on timeout, network errors, 429 and 5xx.
func (s *Store) send(ctx context.Context, c call) (*store.Response, error) {
	h := http.Header{}
	for k, v := range c.header {
		h[k] = v
	}
	u := c.url
	if len(c.query) > 0 {
		u += "?" + c.query.encode()
	}
	req := store.Request{Method: c.method, URL: u, Header: h}
	switch {
	case c.form != nil:
		req.Body = []byte(c.form.encode())
		h["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	case c.json != nil:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(c.json); err != nil {
			return nil, store.Errorf("BilkaToGo: kunne ikke bygge kaldet.")
		}
		req.Body = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		h["Content-Type"] = []string{"application/json"}
	}
	return s.sender.Send(ctx, req)
}

// asDict is _json(): the answer must be a JSON object.
func asDict(resp *store.Response, what string) (obj, error) {
	v, err := decodeJSON(resp.Body)
	if m, ok := v.(map[string]any); err == nil && ok {
		return m, nil
	}
	return nil, store.Errorf("BilkaToGo (%s) svarede uventet. API'et kan have ændret sig.", what)
}
