package main

import (
	"fmt"
	"strings"
)

func releaseBranch(v Version) string { return "chore/release-" + v.String() }

func (e *Env) prepare(arg string) error {
	if err := e.checkOrigin(); err != nil {
		return err
	}
	v, last, err := e.checkTag(arg, true)
	if err != nil {
		return err
	}
	e.logf("preparing %s (previous release %s)", v, last)
	if err := e.checkTree(); err != nil {
		return err
	}
	if err := e.checkCode(); err != nil {
		return err
	}
	branch := releaseBranch(v)
	if _, err := e.write("git", "switch", "-c", branch); err != nil {
		return err
	}
	e.did("created branch " + branch)
	changed, err := bumpPins(e.Root, last, v, e.DryRun)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return fmt.Errorf("no file contains the pin %s: nothing to bump", last)
	}
	e.logf("pins %s -> %s in: %s", last, v, strings.Join(changed, ", "))
	e.did("bumped pins")
	if _, err := e.write("git", append([]string{"add", "--"}, changed...)...); err != nil {
		return err
	}
	if _, err := e.write("git", "commit", "-m", "Pin the install commands to "+v.String()); err != nil {
		return err
	}
	e.did("committed the pins")
	subjects, err := e.git("log", last.String()+"..HEAD", "--format=%s", "--no-merges")
	if err != nil {
		return err
	}
	if _, err := e.write("git", "push", "-u", "origin", branch); err != nil {
		return err
	}
	e.did("pushed " + branch)
	url, err := e.write("gh", "pr", "create", "--repo", repo, "--base", mainName, "--head", branch,
		"--title", "Release "+v.String(), "--body", prBody(last, strings.Split(subjects, "\n")))
	if err != nil {
		return err
	}
	e.did("opened the PR " + url)
	if e.DryRun {
		e.logf("dry-run: would wait for the ci run on %s", branch)
	} else {
		sha, err := e.git("rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if err := e.waitRun("ci", branch, sha); err != nil {
			return err
		}
	}
	e.logf("PR: %s", url)
	e.logf("Merge the PR on GitHub, then run: go run ./tools/release finish %s", v)
	return nil
}

func (e *Env) checkTree() error {
	branch, err := e.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if branch != mainName {
		return fmt.Errorf("must be on %s, but on %q", mainName, branch)
	}
	st, err := e.git("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if st != "" {
		return fmt.Errorf("uncommitted changes:\n%s", st)
	}
	if _, err := e.git("fetch", "origin"); err != nil {
		return err
	}
	local, err := e.git("rev-parse", mainName)
	if err != nil {
		return err
	}
	remote, err := e.git("rev-parse", "origin/"+mainName)
	if err != nil {
		return err
	}
	if local != remote {
		return fmt.Errorf("%s (%s) differs from origin/%s (%s): pull or push first", mainName, local, mainName, remote)
	}
	return nil
}

func (e *Env) checkCode() error {
	steps := [][]string{
		{"go", "vet", "./..."},
		{"gofmt", "-l", "cmd", "internal", "tools"},
		{"go", "test", "-count=1", "./..."},
	}
	for _, s := range steps {
		out, err := e.read(s[0], s[1:]...)
		if err != nil {
			return fmt.Errorf("%s failed:\n%v", strings.Join(s, " "), err)
		}
		if s[0] == "gofmt" && out != "" {
			return fmt.Errorf("gofmt -l lists files that are not formatted:\n%s", out)
		}
		e.logf("ok: %s", strings.Join(s, " "))
	}
	return nil
}
