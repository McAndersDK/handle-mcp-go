package rema1000

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// The pickup store gate (docs/adr/0008). The human chooses one or more stores, in order of
// preference, with `handle-mcp login rema1000`; they are saved on this machine with the session,
// and HANDLE_REMA1000_STORE_ID (a comma-separated list) overrides them. REMA works while at least
// one chosen store has Køb & Hent; the first active one in the human's order is the pickup store.
// The check is repeated at most once an hour, so REMA switches on, or off, by itself. Nothing on
// REMA's side is ever set.
const (
	msgNoStore    = "REMA 1000: ingen butik valgt. Kør: handle-mcp login rema1000"
	msgNoneActive = "REMA 1000: ingen af dine valgte butikker har Køb & Hent endnu. REMA bruges, så snart en af dem får det."
	msgGone       = "REMA 1000: dine valgte butikker findes ikke længere. Vælg igen: handle-mcp login rema1000"

	recheckEvery  = time.Hour
	storePerPage  = 100
	maxStorePages = 10 // 1000 stores: far more than REMA has
	savedKey      = "store_ids"
	oldSavedKey   = "store_id" // sessions saved before the list: one id
)

// pickupStore is the store REMA works with right now.
type pickupStore struct {
	ID   int64
	Name string
	// Earlier is true when a store the human ranked higher exists but has no Køb & Hent yet.
	Earlier bool
}

// gateState is the last verdict, kept until the hour is over or the chosen stores change.
type gateState struct {
	key    string // the chosen ids this verdict is for
	at     time.Time
	pickup *pickupStore
	err    error // a verdict (no store active, stores gone), never a failed call
}

// apiStore is the few facts we read about a store. Phone, address and opening hours are never
// decoded. The postcode and city are read for the human's own terminal (the login list) and are
// never put in a tool answer.
type apiStore struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	City            string `json:"city"`
	PostalCode      string `json:"postal_code"`
	ClickAndCollect bool   `json:"is_click_and_collect_active"`
}

// listStores pages through REMA's store list. enough may stop it early. The list API's own
// filters are not used: only filter[is_click_and_collect_active] is known, and it would hide a
// store that lost Køb & Hent, which the gate must tell from one that is gone.
func (s *Store) listStores(ctx context.Context, enough func([]apiStore) bool) ([]apiStore, error) {
	return pages[apiStore](ctx, s, call{method: "GET", path: "v3/stores", auth: true},
		"butikkerne", storePerPage, maxStorePages, enough)
}

// parseIDs reads "9100,9200" as store ids, in order. Anything that is not a positive number, and
// a repeated id, is dropped.
func parseIDs(raw string) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, part := range strings.Split(raw, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func idsKey(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// savedIDs is the list in a saved session: store_ids, or an old single store_id as a list of one.
func savedIDs(session map[string]string) string {
	if v := strings.TrimSpace(session[savedKey]); v != "" {
		return v
	}
	return strings.TrimSpace(session[oldSavedKey])
}

// storeIDs are the chosen stores in the human's order: the env var first, then the saved session.
func (s *Store) storeIDs() []int64 {
	raw := strings.TrimSpace(os.Getenv(storeEnv))
	if raw == "" {
		s.mu.Lock()
		raw = s.savedStore
		s.mu.Unlock()
	}
	return parseIDs(raw)
}

// judge picks the pickup store from the list: the first chosen store, in the human's order, that
// exists and has Køb & Hent. A chosen id that is not in the list is skipped.
func judge(ids []int64, all []apiStore) (*pickupStore, error) {
	byID := make(map[int64]apiStore, len(all))
	for _, st := range all {
		byID[st.ID] = st
	}
	earlier := false
	for _, id := range ids {
		st, ok := byID[id]
		switch {
		case !ok:
		case st.ClickAndCollect:
			return &pickupStore{ID: id, Name: strings.TrimSpace(st.Name), Earlier: earlier}, nil
		default:
			earlier = true
		}
	}
	if earlier {
		return nil, store.Errorf("%s", msgNoneActive)
	}
	return nil, store.Errorf("%s", msgGone)
}

// ready is the first thing every method does. It logs in if needed and gives the pickup store.
// The stores are read at the first call and again when an hour has passed, whatever the last
// verdict was, so REMA switches on by itself when a chosen store gets Køb & Hent and off when it
// is lost. A failed call (network, login) is not a verdict and is tried again on the next call.
func (s *Store) ready(ctx context.Context) (*pickupStore, error) {
	s.gateLock.Lock()
	defer s.gateLock.Unlock()
	if _, err := s.acquire(ctx, false, ""); err != nil {
		return nil, err
	}
	ids := s.storeIDs()
	if len(ids) == 0 {
		return nil, store.Errorf("%s", msgNoStore)
	}
	key, now := idsKey(ids), s.now()
	s.mu.Lock()
	g := s.gate
	s.mu.Unlock()
	if g != nil && g.key == key {
		if age := now.Sub(g.at); age >= 0 && age < recheckEvery {
			return g.pickup, g.err
		}
	}
	all, err := s.listStores(ctx, func(rows []apiStore) bool { return hasAll(ids, rows) })
	if err != nil {
		return nil, err
	}
	pickup, verdict := judge(ids, all)
	s.mu.Lock()
	s.gate = &gateState{key: key, at: now, pickup: pickup, err: verdict}
	s.mu.Unlock()
	return pickup, verdict
}

// hasAll says whether every chosen id is among the rows, so paging can stop.
func hasAll(ids []int64, rows []apiStore) bool {
	have := make(map[int64]bool, len(rows))
	for _, st := range rows {
		have[st.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return false
		}
	}
	return true
}
