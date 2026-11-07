package nemlig

// Trap 1: Python ran everything on one thread. These tests run the store from many goroutines at
// once. They are written for `go test -race` (CI runs it; there is no gcc on the dev machine):
// an unguarded field would be a data race, and a double login would be a wrong count.

import (
	"context"
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

func TestLoginHappensOnceUnderConcurrency(t *testing.T) {
	for name, saved := range map[string]string{
		"password in env": "",
		"saved session":   `{"email":"kunde@example.com","session":{".ASPXAUTH":"auth-1"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var h *harness
			var s *Store
			if saved == "" {
				h, s = envLogin(t)
			} else {
				isolate(t)
				setSecret(t, saved)
				h, s = newHarness(t)
			}
			errs := make(chan error, 32)
			together(repeat(16, func() {
				_, err := s.GetCart(context.Background())
				errs <- err
			})...)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Errorf("a call failed: %v", err)
				}
			}
			logins := h.f.count("/webapi/login")
			if saved == "" && logins != 1 {
				t.Errorf("%d logins, want 1", logins)
			}
			if saved != "" && logins != 0 {
				t.Errorf("%d password logins with a saved session", logins)
			}
			// one login: two token fetches before and after it (password) or one (session)
			if tokens := h.f.count("/webapi/Token"); tokens != 2 && tokens != 1 {
				t.Errorf("%d token fetches", tokens)
			}
		})
	}
}

func TestEveryMethodAtOnce(t *testing.T) {
	h, s := envLogin(t)
	ctx := context.Background()
	two := 2
	var fns []func()
	for range 4 {
		fns = append(fns,
			func() { _, _ = s.Search(ctx, "mælk", 5) },
			func() { _, _ = s.Offers(ctx, 5) },
			func() { _, _ = s.Recipes(ctx, "kylling", 3) },
			func() { _, _ = s.Recipe(ctx, "kebabfad-98001303", &two) },
			func() { _, _ = s.FeaturedRecipes(ctx, 5) },
			func() { _, _ = s.RecipesByTag(ctx, []string{"Familievenlig"}, nil, 5) },
			func() { _, _ = s.GetCart(ctx) },
			func() { _, _ = s.Terms(ctx) },
			func() { _, _ = s.DeliveryInfo(ctx) },
			func() { _, _ = s.DeliverySlots(ctx, 7) },
			func() { _, _ = s.Orders(ctx, 5) },
			func() { _, _ = s.Order(ctx, "900001") },
			func() { _ = s.Session() },
			func() { _, _ = s.SetQuantities(ctx, nil) },
		)
	}
	together(fns...)
	if n := h.f.count("/webapi/login"); n != 1 {
		t.Errorf("%d logins, want 1", n)
	}
}

// Many calls get 401 at the same moment: they must not all log in again.
func TestRenewalStormDoesNotRelogin(t *testing.T) {
	h, s := envLogin(t)
	if _, err := s.GetCart(context.Background()); err != nil { // log in first
		t.Fatal(err)
	}
	tokensBefore := h.f.count("/webapi/Token")
	h.f.mu.Lock()
	h.f.rejectBearer = map[string]bool{"Bearer bearer-2": true} // the token every call carries expires
	h.f.mu.Unlock()
	errs := make(chan error, 8)
	together(repeat(8, func() {
		_, err := s.GetCart(context.Background())
		errs <- err
	})...)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a call failed: %v", err)
		}
	}
	if n := h.f.count("/webapi/login"); n != 1 {
		t.Errorf("%d logins, want 1: a token renewal was enough", n)
	}
	if renewals := h.f.count("/webapi/Token") - tokensBefore; renewals < 1 || renewals > 8 {
		t.Errorf("%d renewals", renewals)
	}
}

// Without a login the anonymous token is fetched once for everybody.
func TestAnonymousTokenIsFetchedOnce(t *testing.T) {
	isolate(t)
	h, s := newHarness(t)
	together(repeat(16, func() { _, _ = s.Offers(context.Background(), 5) })...)
	if n := h.f.count("/webapi/Token"); n != 1 {
		t.Errorf("%d anonymous tokens, want 1", n)
	}
	if n := h.f.count("/webapi/login"); n != 0 {
		t.Errorf("%d logins without a login", n)
	}
}
