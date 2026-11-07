package bilkatogo

// Python ran everything on one thread. These tests run the store from many goroutines at once.
// They are written for `go test -race` (CI runs it; there is no gcc on the dev machine): an
// unguarded field would be a data race, and a double login would be a wrong count.

import (
	"sync"
	"testing"
)

// together runs every function at the same moment and waits for all.
func together(fns ...func()) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(len(fns))
	done.Add(len(fns))
	for _, fn := range fns {
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

func repeat(n int, fn func()) []func() {
	out := make([]func(), n)
	for i := range out {
		out[i] = fn
	}
	return out
}

func failures(n int, fn func() error) []error {
	errs := make(chan error, n)
	together(repeat(n, func() { errs <- fn() })...)
	close(errs)
	var out []error
	for err := range errs {
		if err != nil {
			out = append(out, err)
		}
	}
	return out
}

func TestLoginHappensOnceUnderConcurrency(t *testing.T) {
	for name, saved := range map[string]string{
		"password in env": "",
		"saved session":   `{"email":"kunde@example.com","session":{"login_token":"st2.fake-login-token"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var h *harness
			if saved == "" {
				h = envLogin(t)
			} else {
				isolate(t)
				setSecret(t, saved)
				h = newHarness(t)
			}
			for _, err := range failures(16, func() error { _, err := h.s.GetCart(bg); return err }) {
				t.Errorf("a call failed: %v", err)
			}
			if n := h.f.count(pLoginJW); n != 1 {
				t.Errorf("%d LoginJWT calls, want 1", n)
			}
			if n := h.f.count(pLogin); saved == "" && n != 1 || saved != "" && n != 0 {
				t.Errorf("%d password logins", n)
			}
		})
	}
}

func TestEveryMethodAtOnce(t *testing.T) {
	h := envLogin(t)
	var fns []func()
	for range 4 {
		fns = append(fns,
			func() { _, _ = h.s.Search(bg, "mælk", 5) },
			func() { _, _ = h.s.Offers(bg, 5) },
			func() { _, _ = h.s.GetCart(bg) },
			func() { _, _ = h.s.Terms(bg) },
			func() { _, _ = h.s.DeliveryInfo(bg) },
			func() { _, _ = h.s.DeliverySlots(bg, 7) },
			func() { _, _ = h.s.Orders(bg, 5) },
			func() { _, _ = h.s.Order(bg, "900000000002") },
			func() { _ = h.s.Session() },
			func() { _, _ = h.s.SetQuantities(bg, nil) },
			func() { _, _ = h.s.Recipes(bg, "x", 1) },
		)
	}
	together(fns...)
	if n := h.f.count(pLoginJW); n != 1 {
		t.Errorf("%d logins, want 1", n)
	}
}

// Many calls get 401 for the same JWT at once: only the first logs in again, the others see that
// the JWT they used is no longer the current one.
func TestExpiredJWTStormLogsInOnce(t *testing.T) {
	h := envLogin(t)
	h.f.numbered = true
	if _, err := h.s.GetCart(bg); err != nil { // jwt-1
		t.Fatal(err)
	}
	h.f.mu.Lock()
	h.f.rejectJWT = map[string]bool{"jwt-1": true}
	h.f.mu.Unlock()
	for _, err := range failures(8, func() error { _, err := h.s.GetCart(bg); return err }) {
		t.Errorf("a call failed: %v", err)
	}
	if n := h.f.count(pLoginJW); n != 2 {
		t.Errorf("%d logins, want 2 (the first, and one for all the 401s)", n)
	}
}

// Both slots and cart are asked for at once, from many callers: still one login.
func TestSlotsInParallelLoginOnce(t *testing.T) {
	h := envLogin(t)
	for _, err := range failures(6, func() error { _, err := h.s.DeliverySlots(bg, 7); return err }) {
		t.Errorf("a call failed: %v", err)
	}
	if n := h.f.count(pLoginJW); n != 1 {
		t.Errorf("%d logins, want 1", n)
	}
}
