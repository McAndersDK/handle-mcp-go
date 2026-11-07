package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Version is a release version, major then minor then patch.
type Version [3]int

var versionRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ParseVersion accepts only vMAJOR.MINOR.PATCH.
func ParseVersion(s string) (Version, error) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("version %q must look like vMAJOR.MINOR.PATCH, for example v1.2.3", s)
	}
	var v Version
	for i := range v {
		fmt.Sscanf(m[i+1], "%d", &v[i])
	}
	return v, nil
}

func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]) }

// Greater reports whether v is a later release than o.
func (v Version) Greater(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] > o[i]
		}
	}
	return false
}

// ParseTags reads `git ls-remote --tags` output and returns every vMAJOR.MINOR.PATCH tag.
func ParseTags(lsRemote string) []Version {
	var out []Version
	for _, line := range strings.Split(lsRemote, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(ref, "refs/tags/"), "^{}")
		if v, err := ParseVersion(name); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// Latest returns the highest version in vs.
func Latest(vs []Version) (Version, bool) {
	var best Version
	found := false
	for _, v := range vs {
		if !found || v.Greater(best) {
			best, found = v, true
		}
	}
	return best, found
}

func contains(vs []Version, v Version) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
