package rema1000

// The pickup store gate (ADR 0008): several chosen stores in order, the first active one is used,
// and the check is repeated once an hour so REMA switches on and off by itself.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// clock is a clock the test moves.
type clock struct{ now time.Time }

func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

// gateStore is a store on the fake with the saved session holding ids, and a clock to move.
func gateStore(t *testing.T, ids string) (*fake, *Store, *clock) {
	t.Helper()
	isolate(t)
	f := newFake(t)
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, savedKey: ids})
	c := &clock{now: testNow}
	s := New(&http.Client{Transport: f},
		WithClock(func() time.Time { return c.now }),
		WithSleep(func(context.Context, time.Duration) error { return nil }))
	return f, s, c
}

const (
	nameMain   = "REMA 1000 Eksempelby" // the fixture's store, id 9100
	nameSecond = "REMA 1000 Sydlig"
)

func secondStore(active bool) fakeStore {
	return fakeStore{ID: 9200, Name: nameSecond, City: "Eksempelby Syd", Post: "0001", Active: active}
}

// listCalls counts the reads of the store list (not the time slots).
func listCalls(f *fake) int {
	n := 0
	for _, r := range f.requests() {
		if r.Method == "GET" && r.Path == "/api/v3/stores" {
			n++
		}
	}
	return n
}

func TestNoStoreChosen(t *testing.T) {
	_, s, _ := gateStore(t, "")
	if _, err := s.GetCart(context.Background()); err == nil || err.Error() != msgNoStore {
		t.Fatalf("got %v", err)
	}
}

func TestFirstInactiveSecondActiveUsesTheSecond(t *testing.T) {
	f, s, _ := gateStore(t, "9100,9200")
	f.storeActive = false
	f.others = []fakeStore{secondStore(true)}
	info, err := s.DeliveryInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.PickupStore == nil || *info.PickupStore != nameSecond {
		t.Fatalf("pickup store: %s", asJSON(t, info))
	}
	want := "Din foretrukne butik har ikke Køb & Hent endnu; bruger " + nameSecond + "."
	if info.Note == nil || !strings.Contains(*info.Note, want) || strings.Contains(*info.Note, nameMain) {
		t.Errorf("note: %v", info.Note)
	}
	if f.count("GET", "/stores/9200/time-slots") != 1 || f.count("GET", "/stores/9100/time-slots") != 0 {
		t.Error("slots were not read from the active store only")
	}
}

func TestOrderDecidesWhenBothAreActive(t *testing.T) {
	f, s, _ := gateStore(t, "9200,9100")
	f.others = []fakeStore{secondStore(true)}
	info, err := s.DeliveryInfo(context.Background())
	if err != nil || info.PickupStore == nil || *info.PickupStore != nameSecond {
		t.Fatalf("%v %s", err, asJSON(t, info))
	}
	if strings.Contains(*info.Note, "foretrukne") {
		t.Errorf("the preferred store is active, so no line: %s", *info.Note)
	}
}

func TestNoneActiveSwitchesOnAfterTheHour(t *testing.T) {
	f, s, c := gateStore(t, "9100,9200")
	f.storeActive = false
	f.others = []fakeStore{secondStore(false)}
	ctx := context.Background()
	if _, err := s.GetCart(ctx); err == nil || err.Error() != msgNoneActive {
		t.Fatalf("got %v", err)
	}
	f.others = []fakeStore{secondStore(true)}
	c.advance(recheckEvery - time.Second) // not yet an hour
	if _, err := s.Search(ctx, "mælk", 3); err == nil || err.Error() != msgNoneActive {
		t.Fatalf("before the hour: %v", err)
	}
	if n := listCalls(f); n != 1 {
		t.Fatalf("%d store list reads before the hour, want 1", n)
	}
	c.advance(time.Second) // exactly an hour
	if _, err := s.Search(ctx, "mælk", 3); err != nil {
		t.Fatalf("after the hour, without a restart: %v", err)
	}
	if n := listCalls(f); n != 2 {
		t.Errorf("%d store list reads, want 2", n)
	}
	for i := 0; i < 3; i++ { // and then it is cached for another hour
		if _, err := s.Search(ctx, "mælk", 3); err != nil {
			t.Fatal(err)
		}
	}
	if n := listCalls(f); n != 2 {
		t.Errorf("%d store list reads, want 2", n)
	}
}

func TestActiveStoreLosingClickAndCollectSwitchesOff(t *testing.T) {
	f, s, c := gateStore(t, "9100")
	ctx := context.Background()
	if _, err := s.GetCart(ctx); err != nil {
		t.Fatal(err)
	}
	f.storeActive = false
	c.advance(recheckEvery - time.Minute)
	if _, err := s.GetCart(ctx); err != nil {
		t.Fatalf("still within the hour: %v", err)
	}
	c.advance(time.Minute)
	if _, err := s.GetCart(ctx); err == nil || err.Error() != msgNoneActive {
		t.Fatalf("got %v", err)
	}
}

func TestChosenStoresThatAreGone(t *testing.T) {
	f, s, c := gateStore(t, "9300,9400")
	ctx := context.Background()
	if _, err := s.GetCart(ctx); err == nil || err.Error() != msgGone {
		t.Fatalf("got %v", err)
	}
	// a gone store is skipped, and it does not count as a better store that is not active
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, savedKey: "9300,9100"})
	s2 := New(&http.Client{Transport: f}, WithClock(func() time.Time { return c.now }))
	info, err := s2.DeliveryInfo(ctx)
	if err != nil || info.PickupStore == nil || *info.PickupStore != nameMain || strings.Contains(*info.Note, "foretrukne") {
		t.Fatalf("%v %s", err, asJSON(t, info))
	}
}

func TestEnvListOverridesTheSavedOne(t *testing.T) {
	f, s, _ := gateStore(t, "9100")
	f.others = []fakeStore{secondStore(true)}
	ctx := context.Background()
	if info, err := s.DeliveryInfo(ctx); err != nil || *info.PickupStore != nameMain {
		t.Fatalf("saved: %v %s", err, asJSON(t, info))
	}
	t.Setenv(storeEnv, "9200, 9100")
	if info, err := s.DeliveryInfo(ctx); err != nil || *info.PickupStore != nameSecond {
		t.Fatalf("env: %v %s", err, asJSON(t, info))
	}
	t.Setenv(storeEnv, "123456") // not in the list
	if _, err := s.GetCart(ctx); err == nil || err.Error() != msgGone {
		t.Fatalf("got %v", err)
	}
	t.Setenv(storeEnv, "x,0,-3") // nothing usable
	if _, err := s.GetCart(ctx); err == nil || err.Error() != msgNoStore {
		t.Fatalf("got %v", err)
	}
}

func TestParseIDs(t *testing.T) {
	for raw, want := range map[string]string{
		"9100": "9100", "9200,9100": "9200,9100", " 9200 , 9100 ": "9200,9100", "9100,9100": "9100",
		"": "", "x": "", "0,-1": "", "9100,,x,9200": "9100,9200",
	} {
		if got := idsKey(parseIDs(raw)); got != want {
			t.Errorf("%q: got %q, want %q", raw, got, want)
		}
	}
}

func TestAnOldSingleStoreIdIsAListOfOne(t *testing.T) {
	isolate(t)
	f := newFake(t)
	saveSession(t, map[string]string{accessCookie: f.access, refreshCookie: f.refreshCookie, oldSavedKey: "9100"})
	s := newStore(f)
	if _, err := s.GetCart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Session(); got[savedKey] != "9100" || got[oldSavedKey] != "" {
		t.Errorf("session: %v", got)
	}
}

func TestChoicesSearchesLocally(t *testing.T) {
	isolate(t)
	f := newFake(t)
	f.storeActive = false
	f.others = []fakeStore{secondStore(true), {ID: 9300, Name: "REMA 1000 Andeby", City: "Andeby", Post: "9999", Active: false}}
	s := newStore(f)
	ctx := store.WithLogin(context.Background(), name, store.Login{Email: "kunde@example.com", Password: "hemmeligt"})
	for query, want := range map[string]string{
		"": "9100,9200,9300", "syd": "9200", "SYD": "9200", "0001": "9200", "andeby": "9300", "eksempelby": "9100,9200",
		"findesikke": "",
	} {
		_, choices, err := s.Choices(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, c := range choices {
			keys = append(keys, c.Key)
		}
		if got := strings.Join(keys, ","); got != want {
			t.Errorf("search %q: got %q, want %q", query, got, want)
		}
	}
	_, choices, _ := s.Choices(ctx, "syd")
	l := choices[0].Label
	if !strings.Contains(l, nameSecond) || !strings.Contains(l, "0001 Eksempelby Syd") || !strings.Contains(l, "Køb & Hent: ja") {
		t.Errorf("label: %q", l)
	}
	_, choices, _ = s.Choices(ctx, "0000")
	if len(choices) != 1 || !strings.Contains(choices[0].Label, "Køb & Hent: nej") {
		t.Errorf("inactive store: %v", choices)
	}
	for _, c := range choices {
		if strings.Contains(c.Label, "98 76") || strings.Contains(c.Label, "Eksempelvej") {
			t.Errorf("a phone or street is in the label: %q", c.Label)
		}
	}
	for _, r := range f.requests() {
		if r.Path == "/api/v3/stores" && strings.Contains(r.Query, "filter") {
			t.Errorf("the list was filtered by the API: %s", r.Query)
		}
	}
}

func TestWithChoicesKeepsTheOrderAndDropsTheOldKey(t *testing.T) {
	isolate(t)
	s := newStore(newFake(t))
	got := s.WithChoices(map[string]string{accessCookie: "a", refreshCookie: "r", oldSavedKey: "9100"},
		[]store.Choice{{Key: "9300"}, {Key: "9100"}})
	if got[savedKey] != "9300,9100" || got[oldSavedKey] != "" || got[accessCookie] != "a" || len(got) != 3 {
		t.Errorf("session: %v", got)
	}
}

// The canary: a store's address, phone, postcode and city never reach a tool answer.
func TestNoStoreAddressPhoneOrCityInAnyAnswer(t *testing.T) {
	f, s, _ := gateStore(t, "9100,9200")
	f.storeActive = false
	f.others = []fakeStore{{ID: 9200, Name: "REMA 1000 Nord", City: "Bynavn Nord", Post: "7777", Active: true}}
	ctx := context.Background()
	slots, err1 := s.DeliverySlots(ctx, 30)
	info, err2 := s.DeliveryInfo(ctx)
	terms, err3 := s.Terms(ctx)
	cart, err4 := s.GetCart(ctx)
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(slots) == 0 {
		t.Fatal("no slots to check")
	}
	all := asJSON(t, []any{slots, info, terms, cart})
	for _, secret := range []string{"Eksempelvej", "+45 98 76 54 32", "98 76", "Bynavn", "7777", "0000", "Eksempelby"} {
		if strings.Contains(all, secret) {
			t.Errorf("%q is in an answer:\n%s", secret, all)
		}
	}
	if !strings.Contains(all, "REMA 1000 Nord") {
		t.Error("the active store's name should be there")
	}
}
