package bilkatogo

// The recipe methods from many goroutines at once, written for `go test -race` (CI runs it; there is
// no gcc on the dev machine). Python ran one thread: the build id and the pages read at once are
// the new shared state.

import (
	"context"
	"testing"
	"time"
)

func TestBuildIDIsReadOnceUnderConcurrency(t *testing.T) {
	h, w := recipeWorld(t)
	for _, err := range failures(16, func() error { _, err := h.s.FeaturedRecipes(bg, 20); return err }) {
		t.Errorf("a call failed: %v", err)
	}
	if n := w.count("/inspiration/opskrifter/"); n != 1 {
		t.Errorf("%d reads of the build id, want 1", n)
	}
}

// A deploy while many calls run: every call ends well, and the new id is read once, not 16 times.
func TestDeployIsReadOnceUnderConcurrency(t *testing.T) {
	h, w := recipeWorld(t)
	if _, err := h.s.FeaturedRecipes(bg, 20); err != nil {
		t.Fatal(err)
	}
	w.setBuild("1800000000")
	for _, err := range failures(16, func() error { _, err := h.s.FeaturedRecipes(bg, 20); return err }) {
		t.Errorf("a call failed: %v", err)
	}
	if n := w.count("/inspiration/opskrifter/"); n != 2 {
		t.Errorf("%d reads of the build id, want 2 (the first, and one for the deploy)", n)
	}
}

func TestAtMostFivePagesAtOnce(t *testing.T) {
	h, w := recipeWorld(t)
	w.delay = 15 * time.Millisecond
	for _, err := range failures(3, func() error { _, err := h.s.RecipesByTag(bg, nil, nil, 20); return err }) {
		t.Errorf("a call failed: %v", err)
	}
	if got := w.maxAtOnce(); got > pagesAtOnce || got < 2 {
		t.Errorf("%d pages at once, want 2 to %d", got, pagesAtOnce)
	}
}

func TestRecipeMethodsAtOnce(t *testing.T) {
	h, w := recipeWorld(t)
	category := "aftensmad/danske-retter"
	var fns []func()
	for range 4 {
		fns = append(fns,
			func() { _, _ = h.s.FeaturedRecipes(bg, 7) },
			func() { _, _ = h.s.RecipesByTag(bg, nil, &category, 5) },
			func() { _, _ = h.s.RecipesByTag(bg, nil, nil, 5) },
			func() { _, _ = h.s.Recipe(bg, karbonader, nil) },
			func() { _, _ = h.s.Recipe(bg, "frokost/findes-ikke", nil) },
			func() { _, _ = h.s.Recipes(bg, "karry", 5) },
			func() { _, _ = h.s.Search(bg, "mælk", 5) },
		)
	}
	together(fns...)
	if n := h.f.count(pLoginJW); n != 1 {
		t.Errorf("%d logins, want 1", n)
	}
	if n := w.count("/inspiration/opskrifter/"); n < 1 {
		t.Errorf("the build id was never read")
	}
}

// A cancelled call stops waiting for a page slot.
func TestCancelledCallStopsWaiting(t *testing.T) {
	h, _ := recipeWorld(t)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := h.s.FeaturedRecipes(ctx, 7); err == nil {
		t.Error("a cancelled call must fail")
	}
}
