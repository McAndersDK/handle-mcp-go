package bilkatogo

// The links to other pages: what a category shows, and the paths people give us.

import (
	"strings"

	"github.com/McAndersDK/handle-mcp-go/internal/pystr"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// card is a page a category shows: its path below pageRoot, its name, and the group it is under.
type card struct {
	path  string
	title string
	group *string
}

// cards is _cards(): the pages a category shows, in page order. Two kinds: the grids of its own or a
// sub-category's pages ("Pita"), and carousels under a heading ("Lav aftensmad med børnene"). A
// recipe's "Lignende opskrifter" is its parent's grid.
func cards(page *pobj) []card {
	own := ownPath(page)
	var group *string
	var order []string
	found := map[string]card{}
	add := func(path, title string, grp *string) { // setdefault: the first card for a path stays
		if _, there := found[path]; !there {
			order = append(order, path)
			found[path] = card{path, title, grp}
		}
	}
	for _, n := range nodes(page, "Heading", "ContentCard", "CarouselContentCard") {
		switch kindOf(n) {
		case "Heading":
			if h := pystr.Strip(n.text("header")); h != "" {
				group = &h
			}
		case "ContentCard":
			grid := n.obj("hierarchy")
			if !strings.HasPrefix(grid.text("path"), own) { // its own sub-pages, or a sub-category's
				continue
			}
			for _, c := range grid.list("children") {
				if child := asNode(c); child != nil {
					g := nameOf(grid)
					add(child.text("path"), nameOf(child), &g)
				}
			}
		case "CarouselContentCard":
			for _, c := range n.list("contentCards") {
				item := asNode(c)
				if item == nil {
					continue
				}
				target := item.obj("page")
				title := item.text("header")
				if title == "" {
					title = nameOf(target)
				}
				add(target.text("path"), pystr.Strip(title), group)
			}
		}
	}
	var out []card
	for _, path := range order {
		c := found[path]
		rel, ok := strings.CutPrefix(path, pageRoot)
		rel = strings.Trim(rel, "/")
		if !ok || path == pageRoot || !isPagePath(rel) { // a path from the page goes into an address: check it
			continue
		}
		c.path = rel
		out = append(out, c)
	}
	return out
}

// isRecipe: a recipe page has an "Ingredienser" heading.
func isRecipe(page *pobj) bool {
	for _, s := range readSections(page) {
		if strings.HasPrefix(s.heading, "Ingrediens") {
			return true
		}
	}
	return false
}

// isPagePath is SLUG: lower-case words of a-z, 0-9 and "-", one or more, joined by "/".
func isPagePath(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func isBilkaHost(h string) bool { return h == "bilkatogo.dk" || h == "www.bilkatogo.dk" }

// recipePath turns 'https://www.bilkatogo.dk/inspiration/opskrifter/aftensmad/pizza/flammkuchen/?x=1',
// '/inspiration/opskrifter/aftensmad/pizza/flammkuchen' or 'aftensmad/pizza/flammkuchen' into
// 'aftensmad/pizza/flammkuchen'. A link to another host is refused, whatever its path says.
func recipePath(recipe string) (string, error) {
	refuse := func() (string, error) {
		return "", store.Errorf("Kender ikke %s. Brug linket fra bilkatogo.dk/inspiration.", pystr.Repr(recipe))
	}
	s := pystr.Lower(pystr.Strip(recipe))
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")
	switch {
	case strings.Contains(s, "://"): // a link: http or https, and our own host
		scheme, rest, _ := strings.Cut(s, "://")
		host, path, _ := strings.Cut(rest, "/")
		if (scheme != "https" && scheme != "http") || !isBilkaHost(host) {
			return refuse()
		}
		s = path
	case strings.HasPrefix(s, "//"):
		host, path, _ := strings.Cut(s[2:], "/")
		if !isBilkaHost(host) {
			return refuse()
		}
		s = path
	default: // "www.bilkatogo.dk/..." or a path
		if first, rest, _ := strings.Cut(strings.TrimLeft(s, "/"), "/"); isBilkaHost(first) {
			s = rest
		}
	}
	s = strings.Trim(s, "/")
	if rest, ok := strings.CutPrefix(s, strings.Trim(pageRoot, "/")); ok && (rest == "" || rest[0] == '/') {
		s = strings.Trim(rest, "/")
	}
	if !isPagePath(s) {
		return refuse()
	}
	return s, nil
}

// categoryPath turns 'Aftensmad', 'aftensmad/danske-retter' or 'børn i køkkenet' into BilkaToGo's path.
func categoryPath(category string) (string, error) {
	p := strings.ReplaceAll(pystr.Lower(pystr.Strip(category)), " ", "-")
	for _, r := range [][2]string{{"æ", "ae"}, {"ø", "oe"}, {"å", "aa"}} {
		p = strings.ReplaceAll(p, r[0], r[1])
	}
	return recipePath(p)
}
