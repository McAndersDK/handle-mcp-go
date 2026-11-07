package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ghPR struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	URL         string `json:"url"`
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

// expectedAssets is the release contents GoReleaser must publish.
func expectedAssets(v Version) []string {
	n := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	out := []string{"checksums.txt"}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if goos == "windows" {
				ext = ".zip"
			}
			out = append(out, fmt.Sprintf("handle-mcp_%s_%s_%s%s", n, goos, arch, ext))
		}
	}
	sort.Strings(out)
	return out
}

// missingAssets returns the expected names that are not in the release.
func missingAssets(v Version, have []string) []string {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	var miss []string
	for _, w := range expectedAssets(v) {
		if !set[w] {
			miss = append(miss, w)
		}
	}
	return miss
}

func (e *Env) finish(arg string) error {
	if err := e.checkOrigin(); err != nil {
		return err
	}
	v, _, err := e.checkTag(arg, false)
	if err != nil {
		return err
	}
	branch := releaseBranch(v)
	out, err := e.gh("pr", "list", "--head", branch, "--state", "all",
		"--json", "number,state,url,mergeCommit", "--limit", "10")
	if err != nil {
		return err
	}
	var prs []ghPR
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return fmt.Errorf("cannot read gh pr list: %w", err)
	}
	if len(prs) == 0 {
		return fmt.Errorf("no PR found for branch %s: run prepare first", branch)
	}
	var pr *ghPR
	for i := range prs {
		if prs[i].State == "MERGED" {
			pr = &prs[i]
			break
		}
	}
	if pr == nil {
		return fmt.Errorf("PR #%d (%s) is %s, not MERGED: merge it on GitHub first", prs[0].Number, prs[0].URL, prs[0].State)
	}
	oid := pr.MergeCommit.OID
	if oid == "" {
		return fmt.Errorf("PR #%d is merged but has no merge commit", pr.Number)
	}
	e.logf("PR #%d merged as %s", pr.Number, oid)
	if _, err := e.git("fetch", "origin"); err != nil {
		return err
	}
	if _, err := e.git("merge-base", "--is-ancestor", oid, "origin/"+mainName); err != nil {
		if re, ok := err.(*RunError); ok && re.Code == 1 {
			return fmt.Errorf("merge commit %s is not on origin/%s", oid, mainName)
		}
		return err
	}
	if _, err := e.write("git", "tag", "-a", v.String(), "-m", "Release "+v.String(), oid); err != nil {
		return err
	}
	e.did("created tag " + v.String() + " on " + oid)
	if _, err := e.write("git", "push", "origin", v.String()); err != nil {
		return err
	}
	e.did("pushed tag " + v.String())
	if e.DryRun {
		e.logf("dry-run: would wait for the release run and check the assets")
	} else {
		if err := e.waitRun("release", v.String(), oid); err != nil {
			return err
		}
		if err := e.checkAssets(v); err != nil {
			return err
		}
	}
	return e.cleanup(branch)
}

func (e *Env) checkAssets(v Version) error {
	out, err := e.gh("release", "view", v.String(), "--json", "assets")
	if err != nil {
		return err
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		return fmt.Errorf("cannot read gh release view: %w", err)
	}
	var have []string
	for _, a := range rel.Assets {
		have = append(have, a.Name)
	}
	if miss := missingAssets(v, have); len(miss) > 0 {
		return fmt.Errorf("release %s misses assets: %s", v, strings.Join(miss, ", "))
	}
	e.logf("release %s has %d assets", v, len(have))
	return nil
}

func (e *Env) cleanup(branch string) error {
	if _, err := e.write("git", "switch", mainName); err != nil {
		return err
	}
	if _, err := e.write("git", "pull", "--ff-only", "origin", mainName); err != nil {
		return err
	}
	if _, err := e.git("rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		if _, err := e.write("git", "branch", "-D", branch); err != nil {
			return err
		}
	}
	if out, err := e.git("ls-remote", "--heads", "origin", branch); err == nil && out != "" {
		if _, err := e.write("git", "push", "origin", "--delete", branch); err != nil {
			return err
		}
	}
	e.logf("done")
	return nil
}
