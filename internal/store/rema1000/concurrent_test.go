package rema1000

// Calls run from many goroutines at once. Written for `go test -race` (CI runs it): an unguarded
// field would be a data race, and a double login or store check would be a wrong count.

import (
	"context"
	"sync"
	"testing"
	"time"
)

func together(n int, fn func()) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			fn()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
}

func TestOneStoreCheckAndNoLoginUnderConcurrency(t *testing.T) {
	f, s := harness(t)
	errs := make(chan error, 32)
	together(16, func() {
		_, err := s.GetCart(context.Background())
		errs <- err
	})
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a call failed: %v", err)
		}
	}
	if n := f.count("GET", "/api/v3/stores"); n != 1 {
		t.Errorf("%d store checks, want 1", n)
	}
	if f.count("POST", "") != 0 {
		t.Error("a login or refresh was made")
	}
}

func TestOneRefreshUnderConcurrency(t *testing.T) {
	isolate(t)
	f := newFake(t)
	saveSession(t, map[string]string{accessCookie: makeJWT(testNow.Add(-time.Minute), userID),
		refreshCookie: "refresh-1", apiCookie: apiValue, awsCookie: lbValue, awsCorsCookie: lbcValue, "store_id": "9100"})
	s := newStore(f)
	together(12, func() {
		if _, err := s.Search(context.Background(), "mælk", 2); err != nil {
			t.Errorf("a call failed: %v", err)
		}
	})
	if n := f.count("POST", "/api/auth/refresh"); n != 1 {
		t.Errorf("%d refreshes, want 1", n)
	}
}

func TestOnePasswordLoginUnderConcurrency(t *testing.T) {
	isolate(t)
	t.Setenv("HANDLE_REMA1000_EMAIL", "kunde@example.com")
	t.Setenv("HANDLE_REMA1000_PASSWORD", "hemmeligt")
	t.Setenv(storeEnv, "9100")
	f := newFake(t)
	s := newStore(f)
	together(8, func() {
		if _, err := s.GetCart(context.Background()); err != nil {
			t.Errorf("a call failed: %v", err)
		}
	})
	if n := f.count("POST", "/oauth2/login"); n != 1 {
		t.Errorf("%d logins, want 1", n)
	}
}
