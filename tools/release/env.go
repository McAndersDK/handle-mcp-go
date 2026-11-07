package main

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	repo      = "McAndersDK/handle-mcp-go"
	mainName  = "main"
	pollEvery = 5 * time.Second
	pollMax   = 36 // about three minutes for a run to show up
)

// Runner runs one program with an argument slice. It never uses a shell.
type Runner interface {
	Run(name string, args ...string) (string, error)
}

// RunError is a command that exited non-zero.
type RunError struct {
	Cmd    string
	Code   int
	Output string
}

func (e *RunError) Error() string {
	return fmt.Sprintf("%s failed (exit %d): %s", e.Cmd, e.Code, strings.TrimSpace(e.Output))
}

type execRunner struct{ dir string }

func (r execRunner) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return strings.TrimSpace(stdout.String()), nil
	}
	code := -1
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return stdout.String(), &RunError{Cmd: name + " " + strings.Join(args, " "), Code: code, Output: stderr.String() + stdout.String()}
}

// Env carries the runner and the flags. Reads always run; writes are printed
// instead of run when DryRun is set.
type Env struct {
	R      Runner
	Out    io.Writer
	Root   string
	DryRun bool
	Sleep  func(time.Duration)
	done   []string
}

func (e *Env) logf(format string, a ...any) { fmt.Fprintf(e.Out, format+"\n", a...) }

// did records a finished step so a failure can say what was already done.
func (e *Env) did(s string) { e.done = append(e.done, s) }

// guard enforces the safety rules on every command, in production and in tests.
func guard(name string, args []string) error {
	for _, a := range args {
		if a == "-f" || strings.HasPrefix(a, "--force") || (a == "--tags" && len(args) > 0 && args[0] == "push") {
			return fmt.Errorf("refusing to run %s with %s", name, a)
		}
	}
	if name == "gh" && len(args) > 0 && args[0] != "--version" {
		ok := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--repo" && args[i+1] == repo {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("refusing to run gh %s without --repo %s", args[0], repo)
		}
	}
	return nil
}

func (e *Env) read(name string, args ...string) (string, error) {
	if err := guard(name, args); err != nil {
		return "", err
	}
	return e.R.Run(name, args...)
}

func (e *Env) write(name string, args ...string) (string, error) {
	if err := guard(name, args); err != nil {
		return "", err
	}
	if e.DryRun {
		e.logf("dry-run: %s %s", name, strings.Join(args, " "))
		return "", nil
	}
	e.logf("run: %s %s", name, strings.Join(args, " "))
	return e.R.Run(name, args...)
}

func (e *Env) git(args ...string) (string, error) { return e.read("git", args...) }

func (e *Env) gh(args ...string) (string, error) {
	return e.read("gh", append(args, "--repo", repo)...)
}

// checkOrigin refuses to run when origin is not the fork.
func (e *Env) checkOrigin() error {
	url, err := e.git("remote", "get-url", "origin")
	if err != nil {
		return err
	}
	u := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	if !strings.HasSuffix(u, "github.com/"+repo) && !strings.HasSuffix(u, "github.com:"+repo) {
		return fmt.Errorf("origin is %q, expected the fork %s", u, repo)
	}
	return nil
}

// checkTag validates the version against the tags on origin and locally, and
// returns the version and the latest existing release.
func (e *Env) checkTag(arg string, needGreater bool) (v, last Version, err error) {
	if v, err = ParseVersion(arg); err != nil {
		return
	}
	out, err := e.git("ls-remote", "--tags", "origin")
	if err != nil {
		return
	}
	tags := ParseTags(out)
	if contains(tags, v) {
		err = fmt.Errorf("tag %s already exists on origin", v)
		return
	}
	local, err := e.git("tag", "--list", v.String())
	if err != nil {
		return
	}
	if strings.TrimSpace(local) != "" {
		err = fmt.Errorf("tag %s already exists locally", v)
		return
	}
	last, found := Latest(tags)
	if needGreater {
		if !found {
			err = fmt.Errorf("no v* release tag on origin, cannot find the previous version to replace")
		} else if !v.Greater(last) {
			err = fmt.Errorf("%s must be greater than the latest tag %s", v, last)
		}
	}
	return
}
