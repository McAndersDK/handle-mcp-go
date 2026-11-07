package bilkatogo

// BilkaToGo recipes: the pages under bilkatogo.dk/inspiration/opskrifter/.
//
// There is no recipe API: the website is a static Nuxt build, and every page has its data in
// /_nuxt/static/<build>/<path>/payload.js (Magnolia CMS; payload.go reads it). The build id changes
// on each deploy, so it is read from the website's HTML and read again when a payload stops
// answering. A recipe has its text as HTML and "Det skal du bruge": a list of Bilka item numbers,
// one pack each. No amounts per product, no persons to scale, no search. Found 2026-11-06, see
// tests/fixtures/bilkatogo/README.md. Public GETs only, and the Algolia search that prices the
// products.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store/jv"
)

const (
	maxDepthCategories = 2 // aftensmad → sandwiches-pita-og-wraps → sandwiches → recipes
	pagesAtOnce        = 5 // pages read at once: be polite to the website
)

var buildRE = regexp.MustCompile(`/_nuxt/static/([0-9]+)`)

// noPageError is "bilkatogo.dk has no page at that path". It is a StoreError, told apart by its type
// (Python matched no error text either).
type noPageError struct{ *store.StoreError }

func (e *noPageError) Unwrap() error { return e.StoreError }

// recipeGuard is the guard of the recipe methods: a shape problem in the product search becomes
// one StoreError, and a "no such page" is handed on as the plain StoreError it is.
func recipeGuard(err *error) {
	if r := recover(); r != nil {
		*err = jv.Recover(r, shapeMessage, "bilkatogo")
	}
	var np *noPageError
	if errors.As(*err, &np) {
		*err = np.StoreError
	}
}

// siteState is what the recipe pages need to remember: the build id, and the pages being read.
type siteState struct {
	lock  sync.Mutex // held while the HTML is read: one read at a time
	build string
	gen   int // changes with every read of the build id

	once  sync.Once
	slots chan struct{}
}

func (st *siteState) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	st.once.Do(func() { st.slots = make(chan struct{}, pagesAtOnce) })
	select {
	case st.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (st *siteState) release() { <-st.slots }

// buildID is the build id to use. `used` is the generation of the id the caller just saw fail (0 =
// the first call): another call may have read a newer id while this one waited for the lock, and
// then there is nothing to read again.
func (s *Store) buildID(ctx context.Context, used int) (string, int, error) {
	st := &s.site
	st.lock.Lock()
	defer st.lock.Unlock()
	if st.build != "" && st.gen != used {
		return st.build, st.gen, nil
	}
	resp, err := s.send(withoutCart(ctx), call{method: "GET", url: siteURL + pageRoot})
	if err != nil {
		return "", 0, err
	}
	var m [][]byte
	if resp.Status == 200 {
		m = buildRE.FindSubmatch(resp.Body)
	}
	if len(m) < 2 {
		return "", 0, store.Errorf("BilkaToGo-opskrifter har ændret format: ingen build.")
	}
	st.build, st.gen = string(m[1]), st.gen+1
	return st.build, st.gen, nil
}

// page is the data of one page. At most pagesAtOnce are read at a time.
func (s *Store) page(ctx context.Context, path string) (*pobj, error) {
	if err := s.site.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.site.release()
	return s.payload(ctx, path)
}

// payload reads a page. A stale build id, like an unknown page, answers with the website's HTML:
// read the id again, once, and then the page is not there.
func (s *Store) payload(ctx context.Context, path string) (*pobj, error) {
	build, gen, err := s.buildID(ctx, 0)
	for attempt := 0; attempt < 2; attempt++ {
		if err != nil {
			return nil, err
		}
		url := siteURL + "/_nuxt/static/" + build + strings.TrimRight(pageRoot+path, "/") + "/payload.js"
		resp, serr := s.send(withoutCart(ctx), call{method: "GET", url: url})
		if serr != nil {
			return nil, serr
		}
		if resp.Status == 200 && bytes.HasPrefix(resp.Body, []byte("__NUXT_JSONP__")) {
			return pageOf(resp.Body)
		}
		if attempt == 0 {
			build, gen, err = s.buildID(ctx, gen)
		}
	}
	return nil, &noPageError{&store.StoreError{Msg: fmt.Sprintf("BilkaToGo har ingen side %s%s/.", pageRoot, path)}}
}

// pageOf is data[0].page of a payload.
func pageOf(body []byte) (*pobj, error) {
	top, err := parsePayload(string(body))
	if err != nil {
		return nil, err
	}
	data := top.list("data")
	if len(data) == 0 {
		return nil, errFormat()
	}
	first := asNode(data[0])
	if first == nil {
		return nil, errFormat()
	}
	if page := first.obj("page"); page != nil {
		return page, nil
	}
	return nil, errFormat()
}

// newRecipe is _recipe(): a recipe in a list.
func newRecipe(c card) model.Recipe {
	return model.Recipe{Store: name, ID: c.path, Name: c.title, URL: siteURL + pageRoot + c.path + "/", Group: c.group}
}

func head[T any](list []T, limit int) []T {
	return list[:min(max(limit, 0), len(list))]
}

// Recipes: BilkaToGo has no recipe search.
func (s *Store) Recipes(context.Context, string, int) ([]model.Recipe, error) {
	return nil, store.Errorf("BilkaToGo har ingen opskriftssøgning. Brug list_recipes med category, " +
		"fx aftensmad eller aftensmad/danske-retter.")
}

// FeaturedRecipes is this week's "Madplan": 7 dinners, Monday to Sunday, in that order.
func (s *Store) FeaturedRecipes(ctx context.Context, limit int) (_ []model.Recipe, err error) {
	defer recipeGuard(&err)
	page, err := s.page(ctx, featured)
	if err != nil {
		return nil, err
	}
	var found []model.Recipe
	for _, c := range cards(page) {
		found = append(found, newRecipe(c))
	}
	if len(found) == 0 {
		return nil, store.Errorf("Fandt ingen opskrifter i BilkaToGos madplan.")
	}
	return head(found, limit), nil
}

// RecipesByTag lists all recipes in one of BilkaToGo's categories, e.g. "aftensmad" or
// "aftensmad/danske-retter". A category with sub-categories gives their recipes, mixed. There are no
// tags.
func (s *Store) RecipesByTag(ctx context.Context, tags []string, category *string, limit int) (_ []model.Recipe, err error) {
	defer recipeGuard(&err)
	if len(tags) > 0 {
		return nil, store.Errorf("BilkaToGo har ingen tags. Brug category, fx aftensmad, aftensmad/pastaretter, " +
			"boern-i-koekkenet eller nemme.")
	}
	shown := "None" // how the category is told back in an error
	path := "aftensmad"
	if category != nil {
		shown = pystr.Repr(*category)
		if *category != "" {
			p, err := categoryPath(*category)
			if err != nil {
				return nil, err
			}
			path = p
		}
	}
	groups, err := s.collect(ctx, path, maxDepthCategories, &seenPages{pages: map[string]*pobj{}})
	var np *noPageError
	if errors.As(err, &np) {
		top, topErr := s.topCategories(ctx)
		if topErr != nil {
			return nil, topErr
		}
		return nil, store.Errorf("BilkaToGo kender ikke kategorien %s. Brug fx: %s.", shown, strings.Join(top, ", "))
	}
	if err != nil {
		return nil, err
	}
	mixed := mix(groups) // one from each group, in turn
	if len(mixed) == 0 {
		return nil, store.Errorf("Fandt ingen opskrifter i BilkaToGos kategori %s.", shown)
	}
	return head(mixed, limit), nil
}
