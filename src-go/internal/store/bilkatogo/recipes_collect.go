package bilkatogo

// Walking a category: the recipes below a path, grouped by sub-category.

import (
	"context"
	"sync"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
)

// seenPages are the pages one walk has read, so the first card is not read twice.
type seenPages struct {
	mu    sync.Mutex
	pages map[string]*pobj
}

func (sp *seenPages) get(path string) *pobj {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.pages[path]
}

func (sp *seenPages) put(path string, page *pobj) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.pages[path] = page
}

// collect is _collect(): the recipes per group below `path`. The first card shows whether the cards
// are recipes or sub-categories (aftensmad → pizza → recipes). The sub-categories are read at the
// same time, pagesAtOnce at a time. The error is the one of the first card (by order) that failed.
func (s *Store) collect(ctx context.Context, path string, depth int, seen *seenPages) ([][]model.Recipe, error) {
	page := seen.get(path)
	if page == nil {
		var err error
		if page, err = s.page(ctx, path); err != nil {
			return nil, err
		}
	}
	list := cards(page)
	if len(list) == 0 {
		return nil, nil
	}
	if depth > 0 { // at depth 0 the cards are taken for recipes without a look
		first, err := s.page(ctx, list[0].path)
		if err != nil {
			return nil, err
		}
		seen.put(list[0].path, first)
		if !isRecipe(first) {
			return s.collectEach(ctx, list, depth-1, seen)
		}
	}
	var order []string
	groups := map[string][]model.Recipe{}
	for _, c := range list {
		key := "\x00" // no group
		if c.group != nil {
			key = "g" + *c.group
		}
		if _, there := groups[key]; !there {
			order = append(order, key)
		}
		groups[key] = append(groups[key], newRecipe(c))
	}
	out := make([][]model.Recipe, len(order))
	for i, key := range order {
		out[i] = groups[key]
	}
	return out, nil
}

func (s *Store) collectEach(ctx context.Context, list []card, depth int, seen *seenPages) ([][]model.Recipe, error) {
	found := make([][][]model.Recipe, len(list))
	errs := make([]error, len(list))
	var wg sync.WaitGroup
	for i, c := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found[i], errs[i] = s.collect(ctx, c.path, depth, seen)
		}()
	}
	wg.Wait()
	var out [][]model.Recipe
	for i := range list {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out = append(out, found[i]...)
	}
	return out, nil
}

// topCategories are the paths of the categories the recipe front page shows.
func (s *Store) topCategories(ctx context.Context) ([]string, error) {
	page, err := s.page(ctx, "")
	if err != nil {
		return nil, err
	}
	var top []string
	for _, c := range cards(page) {
		top = append(top, c.path)
	}
	return top, nil
}

// mix is zip_longest: the first of every group, then the second of every group, and so on.
func mix(groups [][]model.Recipe) []model.Recipe {
	var out []model.Recipe
	for i := 0; ; i++ {
		more := false
		for _, g := range groups {
			if i < len(g) {
				out = append(out, g[i])
				more = true
			}
		}
		if !more {
			return out
		}
	}
}
