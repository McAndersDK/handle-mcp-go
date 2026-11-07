package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// bumpText replaces the old version where it is an install pin or a release
// link: "@vA.B.C", "releases/tag/vA.B.C" and "[the vA.B.C release]".
func bumpText(text string, old, next Version) string {
	re := regexp.MustCompile(`(@|releases/tag/|\[the )` + regexp.QuoteMeta(old.String()) + `([^0-9]|$)`)
	return re.ReplaceAllString(text, "${1}"+next.String()+"${2}")
}

// bumpPins rewrites README.md and docs/*.md (not docs/adr/) under root and
// returns the files that changed. Nothing is written when dryRun is set.
func bumpPins(root string, old, next Version, dryRun bool) ([]string, error) {
	files := []string{"README.md"}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(docs)
	for _, d := range docs {
		files = append(files, "docs/"+filepath.Base(d))
	}
	var changed []string
	for _, f := range files {
		path := filepath.Join(root, filepath.FromSlash(f))
		b, err := os.ReadFile(path)
		if err != nil {
			return changed, err
		}
		text := string(b)
		out := bumpText(text, old, next)
		if out == text {
			continue
		}
		if !dryRun {
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				return changed, err
			}
		}
		changed = append(changed, f)
	}
	return changed, nil
}

// prBody lists the commit subjects since the last release as plain text.
func prBody(last Version, subjects []string) string {
	var b strings.Builder
	b.WriteString("Changes since " + last.String() + ":\n\n")
	n := 0
	for _, s := range subjects {
		if s = strings.TrimSpace(s); s != "" {
			b.WriteString("- " + s + "\n")
			n++
		}
	}
	if n == 0 {
		b.WriteString("- No commits since the last release.\n")
	}
	return b.String()
}
