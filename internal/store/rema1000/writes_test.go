package rema1000

// Tests for the safe-write rules in docs/adr/0008: no retry of a POST that creates something,
// one write at a time, and no guessing between duplicate lists or lines.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

const today = "handle-mcp 2026-11-07"

func set(t *testing.T, s *Store, items ...store.CartItem) []string {
	t.Helper()
	res, err := s.SetQuantities(context.Background(), items)
	if err != nil {
		t.Fatalf("SetQuantities: %v", err)
	}
	var bad []string
	for _, r := range res {
		if !r.OK {
			bad = append(bad, asJSON(t, r))
		}
	}
	return bad
}

func TestLostAnswerOnCreatingTheListDoesNotCreateASecond(t *testing.T) {
	for _, kind := range []string{"500", "net"} {
		f, s := harness(t)
		f.lose = map[string]string{"POST list": kind}
		if bad := set(t, s, store.CartItem{ProductID: "20302", Quantity: 2}); len(bad) > 0 {
			t.Errorf("%s: not ok: %v", kind, bad)
		}
		if n := f.count("POST", "/api/v2/shoppinglists"); n != 2 { // the list POST and the item POST
			t.Errorf("%s: %d POSTs, want 2", kind, n)
		}
		if len(f.lists) != 1 || len(f.lists[0].Items) != 1 {
			t.Errorf("%s: lists %+v", kind, f.lists)
		}
	}
}

func TestLostAnswerOnAddingALineDoesNotAddItTwice(t *testing.T) {
	for _, kind := range []string{"500", "net"} {
		f, s := harness(t)
		l := f.addList(today, false)
		f.lose = map[string]string{"POST items": kind}
		if bad := set(t, s, store.CartItem{ProductID: "555", Quantity: 3}); len(bad) > 0 {
			t.Errorf("%s: not ok: %v", kind, bad)
		}
		if n := f.count("POST", "/items"); n != 1 {
			t.Errorf("%s: %d item POSTs, want 1", kind, n)
		}
		if len(l.Items) != 1 || l.Items[0].Amount != 3 {
			t.Errorf("%s: items %+v", kind, l.Items)
		}
	}
}

func TestLostAnswerThatDidNotLandIsPostedAgainAfterARead(t *testing.T) {
	f, s := harness(t)
	f.addList(today, false)
	f.lose = map[string]string{"POST items": "500"}
	f.loseWithoutEffect = true
	if bad := set(t, s, store.CartItem{ProductID: "555", Quantity: 3}); len(bad) > 0 {
		t.Fatalf("not ok: %v", bad)
	}
	if n := f.count("POST", "/items"); n != 2 {
		t.Errorf("%d item POSTs, want 2", n)
	}
	if len(f.lists[0].Items) != 1 {
		t.Errorf("items %+v", f.lists[0].Items)
	}
}

func TestTwoWritesOnANewDayCreateOneList(t *testing.T) {
	f, s := harness(t)
	f.listDelay = 30 * time.Millisecond // both calls look for the list before either has made it
	var mu sync.Mutex
	var errs []error
	together(2, func() {
		_, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "20302", Quantity: 1}})
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	for _, err := range errs {
		if err != nil {
			t.Errorf("a call failed: %v", err)
		}
	}
	if len(f.lists) != 1 {
		t.Fatalf("%d lists, want 1", len(f.lists))
	}
	if n := len(f.lists[0].Items); n != 1 {
		t.Errorf("%d lines: %+v", n, f.lists[0].Items)
	}
}

func TestSeveralListsWithTodaysNameWriteNothing(t *testing.T) {
	f, s := harness(t)
	f.addList(today, false, fakeItem{ID: 1, StoreItemID: 111, Amount: 1})
	f.addList(today, false)
	_, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "111", Quantity: 5}, {ProductID: "222", Quantity: 1}})
	wantStoreError(t, err, "flere lister")
	_, err = s.GetCart(context.Background())
	wantStoreError(t, err, "flere lister")
	for _, r := range f.requests() {
		if r.Method != "GET" {
			t.Errorf("a write was sent: %s %s", r.Method, r.Path)
		}
	}
}

func TestAProductOnSeveralLinesIsTheirSum(t *testing.T) {
	f, s := harness(t)
	l := f.addList(today, false, fakeItem{ID: 10, StoreItemID: 111, Amount: 1}, fakeItem{ID: 11, StoreItemID: 111, Amount: 2},
		fakeItem{ID: 12, StoreItemID: 222, Amount: 1})
	cart, err := s.GetCart(context.Background())
	if err != nil || len(cart.Lines) != 2 || cart.Lines[0].ProductID != "111" || cart.Lines[0].Quantity != 3 {
		t.Fatalf("cart %s, err %v", asJSON(t, cart.Lines), err)
	}
	// 3 is already the sum, but the lines are made one: the first is set, the extra removed
	if bad := set(t, s, store.CartItem{ProductID: "111", Quantity: 3}); len(bad) > 0 {
		t.Fatalf("not ok: %v", bad)
	}
	if len(l.Items) != 2 || l.Items[0].Amount != 3 || l.Items[1].StoreItemID != 222 {
		t.Errorf("items %+v", l.Items)
	}
}

func TestSettingAProductOnSeveralLinesSetsTheFirstAndRemovesTheExtras(t *testing.T) {
	f, s := harness(t)
	l := f.addList(today, false, fakeItem{ID: 10, StoreItemID: 111, Amount: 1}, fakeItem{ID: 11, StoreItemID: 111, Amount: 2},
		fakeItem{ID: 12, StoreItemID: 111, Amount: 4}, fakeItem{ID: 13, StoreItemID: 222, Amount: 1})
	res, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "111", Quantity: 5}, {ProductID: "222", Quantity: 0}})
	if err != nil || !res[0].OK || *res[0].Applied != 5 || !res[1].OK {
		t.Fatalf("result %s, err %v", asJSON(t, res), err)
	}
	if len(l.Items) != 1 || l.Items[0].ID != 10 || l.Items[0].Amount != 5 {
		t.Errorf("items %+v", l.Items)
	}
	l2 := f.addList("Min liste", true, fakeItem{ID: 1, StoreItemID: 111, Amount: 1})
	_ = l2
	if _, err := s.SetQuantities(context.Background(), []store.CartItem{{ProductID: "111", Quantity: 0}}); err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 0 || len(f.lists[1].Items) != 1 {
		t.Errorf("items %+v, other %+v", l.Items, f.lists[1].Items)
	}
}
