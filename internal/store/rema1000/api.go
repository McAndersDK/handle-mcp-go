package rema1000

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// call is one API request. auth adds the Bearer token (and renews it once on a 401). The public
// calls (search, offers, recipes) go without it: the answers are the same logged in and out.
type call struct {
	method string
	path   string
	query  url.Values
	body   any // sent as JSON
	auth   bool
	once   bool // never retried: a POST that creates something (see setqty.go)
}

// envelope is the shape of every list answer: data and a page count.
type envelope[T any] struct {
	Data T `json:"data"`
	Meta struct {
		Pagination struct {
			LastPage int `json:"last_page"`
		} `json:"pagination"`
	} `json:"meta"`
}

// api sends one request. A 401 renews the token once; a second one means the session is over.
// A redirect or a 403 is a queue or a bot check: it is reported, never worked around. Other
// statuses are returned for the caller to judge.
func (s *Store) api(ctx context.Context, c call) (*store.Response, error) {
	var data []byte
	if c.body != nil {
		var err error
		if data, err = json.Marshal(c.body); err != nil {
			return nil, store.Errorf("REMA 1000: forespørgslen kunne ikke bygges.")
		}
	}
	u := apiBase + "/" + c.path
	if len(c.query) > 0 {
		u += "?" + c.query.Encode()
	}
	used := ""
	for attempt := 0; ; attempt++ {
		token := ""
		if c.auth {
			var err error
			if token, err = s.acquire(ctx, attempt > 0, used); err != nil {
				return nil, err
			}
			used = token
		}
		h := apiHeaders(token)
		if data != nil {
			h.Set("Content-Type", "application/json")
		}
		resp, err := s.sender.Send(ctx, store.Request{Method: c.method, URL: u, Header: h, Body: data, NoRetry: c.once})
		if err != nil {
			return nil, err
		}
		switch {
		case resp.Status == 401 && c.auth && attempt == 0:
			continue
		case resp.Status == 401:
			return nil, store.Expired(name)
		case resp.Status == 403:
			return nil, errBot
		case resp.Status >= 300 && resp.Status < 400:
			return nil, errQueue
		}
		return resp, nil
	}
}

func isOK(resp *store.Response) bool { return resp.Status >= 200 && resp.Status < 300 }

// getJSON sends the request and decodes a 2xx answer into out. what names the call in errors.
func (s *Store) getJSON(ctx context.Context, c call, what string, out any) error {
	resp, err := s.api(ctx, c)
	if err != nil {
		return err
	}
	if !isOK(resp) {
		return store.Errorf("REMA 1000 afviste %s (%d).", what, resp.Status)
	}
	return decode(resp, what, out)
}

func decode(resp *store.Response, what string, out any) error {
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return store.Errorf("REMA 1000 svarede med noget uventet (%s). API'et kan have ændret sig.", what)
	}
	return nil
}

// pages reads a paged list: up to max pages of per_page rows each, stopping at the last page or
// when `enough` says the rows so far are enough.
func pages[T any](ctx context.Context, s *Store, c call, what string, perPage, maxPages int, enough func(rows []T) bool) ([]T, error) {
	var all []T
	for page := 1; page <= maxPages; page++ {
		q := url.Values{}
		for k, v := range c.query {
			q[k] = v
		}
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))
		c.query = q
		var env envelope[[]T]
		if err := s.getJSON(ctx, c, what, &env); err != nil {
			return nil, err
		}
		all = append(all, env.Data...)
		if len(env.Data) == 0 || page >= env.Meta.Pagination.LastPage || (enough != nil && enough(all)) {
			break
		}
	}
	return all, nil
}
