package bilkatogo

// Reading one recipe page, as the Magnolia CMS tree in payload.js has it. Nothing here touches the
// network or panics: a field of the wrong type is read as missing.

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/pystr"
)

const (
	siteURL   = "https://www.bilkatogo.dk"
	pageRoot  = "/inspiration/opskrifter/"
	imagesURL = "https://digitalassets.sallinggroup.com/image/upload/w_1000,dpr_1,c_fill,q_auto,f_auto/" +
		"t_DigitalAiLogo/external_content_providers/magnolia/next/"
	featured = "madplaner" // "Ugens madplan": 7 dinners, Monday to Sunday
)

var (
	tagRE     = regexp.MustCompile(`<[^>]+>`)
	h2RE      = regexp.MustCompile(`(?s)<h2>.*?</h2>`)
	liRE      = regexp.MustCompile(`(?s)<li[^>]*>(.*?)</li>`)
	paraRE    = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	brRE      = regexp.MustCompile(`<br\s*/?>`)
	numberRE  = regexp.MustCompile(`[0-9]+`)
	jcrRE     = regexp.MustCompile(`^jcr:[A-Za-z0-9-]+$`)
	methodHds = []string{"Fremgangsm", "Sådan gør du"} // the heading above the steps
)

// --- reading the tree ---------------------------------------------------------

func (o *pobj) text(key string) string {
	s, _ := o.get(key).(string)
	return s
}

func (o *pobj) obj(key string) *pobj {
	n, _ := o.get(key).(*pobj)
	return n
}

func (o *pobj) list(key string) []any {
	l, _ := o.get(key).([]any)
	return l
}

func asNode(v any) *pobj {
	n, _ := v.(*pobj)
	return n
}

// nodes is every Magnolia component of these kinds, in page order (a node before what is in it).
func nodes(root any, kinds ...string) []*pobj {
	var found []*pobj
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case *pobj:
			kind := strings.TrimPrefix(x.text("componentType"), "MagnoliaComponents")
			for _, k := range kinds {
				if k == kind {
					found = append(found, x)
					break
				}
			}
			for _, key := range x.keys {
				walk(x.vals[key])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(root)
	return found
}

func kindOf(n *pobj) string {
	return strings.TrimPrefix(n.text("componentType"), "MagnoliaComponents")
}

// plain is _text(): the words of an HTML fragment, one space apart.
func plain(fragment string) string {
	text := html.UnescapeString(tagRE.ReplaceAllString(fragment, " "))
	return strings.Join(strings.FieldsFunc(text, pystr.IsSpace), " ") // a no-break space is a space too
}

func nameOf(page *pobj) string {
	n := page.text("displayName")
	if n == "" {
		n = page.text("title")
	}
	return pystr.Strip(n)
}

// ownPath is the page's own path as the site writes it: no "/bilka-to-go", one slash at the end.
func ownPath(page *pobj) string {
	return strings.TrimRight(strings.TrimPrefix(page.text("path"), "/bilka-to-go"), "/") + "/"
}

// --- text sections --------------------------------------------------------------

type section struct {
	heading string
	lines   []string
}

// sections keeps the headings in the order the page has them.
type sections []*section

func (ss *sections) at(heading string) *section {
	for _, s := range *ss {
		if s.heading == heading {
			return s
		}
	}
	s := &section{heading: heading}
	*ss = append(*ss, s)
	return s
}

// first is the lines under the first heading that starts with one of the prefixes.
func (ss sections) first(prefixes ...string) []string {
	for _, s := range ss {
		for _, p := range prefixes {
			if strings.HasPrefix(s.heading, p) {
				return s.lines
			}
		}
	}
	return nil
}

// readSections is _sections(): the recipe's text by heading ("Ingredienser", "Fremgangsmåde" ...). The
// text before the first heading is under "".
func readSections(page *pobj) sections {
	ss := sections{}
	heading := ""
	ss.at(heading)
	for _, para := range nodes(page, "Paragraph") {
		for _, part := range splitH2(para.text("htmlContent")) {
			if strings.HasPrefix(part, "<h2>") {
				heading = plain(part)
				ss.at(heading)
				continue
			}
			s := ss.at(heading)
			for _, ln := range partLines(part) {
				if t := plain(ln); t != "" {
					s.lines = append(s.lines, t)
				}
			}
		}
	}
	return ss
}

// splitH2 is re.split("(<h2>.*?</h2>)"): the text between the headings, and the headings.
func splitH2(src string) []string {
	var parts []string
	last := 0
	for _, m := range h2RE.FindAllStringIndex(src, -1) {
		parts = append(parts, src[last:m[0]], src[m[0]:m[1]])
		last = m[1]
	}
	return append(parts, src[last:])
}

// partLines: the <li> items, or else the <p> lines split by <br>. A <strong> line is a sub-heading
// and an <em> paragraph is a note: neither is a line of the recipe.
func partLines(part string) []string {
	var lines []string
	if strings.Contains(part, "<li") {
		for _, m := range liRE.FindAllStringSubmatch(part, -1) {
			lines = append(lines, m[1])
		}
		return lines
	}
	for _, m := range paraRE.FindAllStringSubmatch(part, -1) {
		p := m[1]
		if strings.HasPrefix(strings.TrimLeftFunc(p, pystr.IsSpace), "<em>") {
			continue
		}
		for _, ln := range brRE.Split(p, -1) {
			t := pystr.Strip(ln)
			if len(t) >= len("<strong></strong>") && strings.HasPrefix(t, "<strong>") && strings.HasSuffix(t, "</strong>") {
				continue
			}
			lines = append(lines, ln)
		}
	}
	return lines
}

// --- facts, image, item numbers -----------------------------------------------

// facts reads "4 personer" and "35 minutter" from the row of facts under the title. A recipe
// written for "16 stk." or "1 glas" has no persons.
func facts(page *pobj) (persons *int, minutes *string) {
	for _, usp := range nodes(page, "FrontpageUsps") {
		for _, e := range usp.list("contentElements") {
			el := asNode(e)
			title := pystr.Lower(pystr.Strip(el.text("title")))
			if n := numberRE.FindString(title); strings.HasSuffix(title, "personer") && n != "" {
				if v, err := strconv.Atoi(n); err == nil {
					persons = &v
				}
			} else if strings.Contains(title, "minut") || strings.Contains(title, "time") { // "35 minutter", "2,5 timer"
				m := strings.ReplaceAll(strings.ReplaceAll(title, "minutter", "min"), "minut", "min")
				minutes = &m
			}
		}
	}
	return persons, minutes
}

// image is the first banner's picture. Only a Magnolia asset id goes into the address.
func image(page *pobj) *string {
	banners := nodes(page, "Banner")
	if len(banners) == 0 {
		return nil
	}
	jcr := banners[0].text("imageJCR")
	if !jcrRE.MatchString(jcr) {
		return nil
	}
	url := imagesURL + jcr
	return &url
}

// isItemNumber: Bilka item numbers are digits, and only digits go into an Algolia filter.
func isItemNumber(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// itemNumbers is "Det skal du bruge": the curated lists' item numbers, in page order, once each.
func itemNumbers(page *pobj) []string {
	var skus []string
	seen := map[string]bool{}
	for _, slider := range nodes(page, "CuratedSlider") {
		for _, v := range slider.obj("sourceProperties").list("curatedSkuList") {
			if s, ok := v.(string); ok && isItemNumber(s) && !seen[s] { // a hole (void 0) or a number is skipped
				seen[s] = true
				skus = append(skus, s)
			}
		}
	}
	return skus
}
