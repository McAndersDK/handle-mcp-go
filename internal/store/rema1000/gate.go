package rema1000

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// The pickup store gate (docs/adr/0008). REMA only works with a known store that has Køb & Hent.
// The human chooses it once with `handle-mcp login rema1000`; it is saved on this machine with
// the session, and HANDLE_REMA1000_STORE_ID overrides it. Nothing on REMA's side is ever set.
const (
	msgNoStore     = "REMA 1000: ingen afhentningsbutik med Køb & Hent er valgt. Kør: handle-mcp login rema1000"
	msgNoClickColl = "REMA 1000: din afhentningsbutik har ikke Køb & Hent længere. Vælg en anden: handle-mcp login rema1000"
	maxStorePages  = 30
)

// pickupStore is the chosen store, once it has been checked.
type pickupStore struct {
	ID   int64
	Name string
}

// apiStore is the few facts we read about a store. Phone, address and opening hours are never
// decoded: a store's answer must not carry them on.
type apiStore struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	City            string `json:"city"`
	ClickAndCollect bool   `json:"is_click_and_collect_active"`
}

// clickAndCollectStores lists the stores with Køb & Hent active, read with the login.
func (s *Store) clickAndCollectStores(ctx context.Context) ([]apiStore, error) {
	c := call{method: "GET", path: "v3/stores", auth: true,
		query: url.Values{"filter[is_click_and_collect_active]": {"true"}}}
	all, err := pages[apiStore](ctx, s, c, "butikkerne", 100, maxStorePages, nil)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, st := range all {
		if st.ClickAndCollect { // the filter is REMA's; the flag is the rule
			out = append(out, st)
		}
	}
	return out, nil
}

// storeID is the chosen store: the env var first, then the saved session. 0 when none.
func (s *Store) storeID() int64 {
	raw := strings.TrimSpace(os.Getenv(storeEnv))
	if raw == "" {
		s.mu.Lock()
		raw = s.savedStore
		s.mu.Unlock()
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// ready is the first thing every method does. It logs in if needed, and checks once per process
// that the chosen store exists and still has Køb & Hent. A store that fails the check is checked
// again on the next call, so a fixed setting is picked up without a restart.
func (s *Store) ready(ctx context.Context) (*pickupStore, error) {
	s.gateLock.Lock()
	defer s.gateLock.Unlock()
	if _, err := s.acquire(ctx, false, ""); err != nil {
		return nil, err
	}
	s.mu.Lock()
	known := s.gate
	s.mu.Unlock()
	id := s.storeID()
	if known != nil && known.ID == id {
		return known, nil
	}
	if id == 0 {
		return nil, store.Errorf("%s", msgNoStore)
	}
	stores, err := s.clickAndCollectStores(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range stores {
		if st.ID == id {
			p := &pickupStore{ID: id, Name: strings.TrimSpace(st.Name)}
			s.mu.Lock()
			s.gate = p
			s.mu.Unlock()
			return p, nil
		}
	}
	return nil, store.Errorf("%s", msgNoClickColl)
}

// Choices is the first half of `handle-mcp login rema1000`: it logs in with the login in ctx and
// lists the stores with Køb & Hent, for the human to pick one.
func (s *Store) Choices(ctx context.Context) (string, []store.Choice, error) {
	stores, err := s.clickAndCollectStores(ctx)
	if err != nil {
		return "", nil, err
	}
	choices := make([]store.Choice, 0, len(stores))
	for _, st := range stores {
		label := strings.TrimSpace(st.Name)
		if city := strings.TrimSpace(st.City); city != "" && !strings.Contains(label, city) {
			label += ", " + city
		}
		choices = append(choices, store.Choice{Key: strconv.FormatInt(st.ID, 10), Label: label})
	}
	return "Vælg den REMA 1000-butik, du henter i (kun butikker med Køb & Hent):", choices, nil
}

// WithChoice adds the chosen store id to the session that is saved. It is kept on this machine.
func (s *Store) WithChoice(session map[string]string, c store.Choice) map[string]string {
	out := make(map[string]string, len(session)+1)
	for k, v := range session {
		out[k] = v
	}
	out["store_id"] = c.Key
	return out
}
