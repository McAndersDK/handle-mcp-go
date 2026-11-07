// Command release prepares and finishes a handle-mcp-go release. It is a
// developer tool and is never shipped: GoReleaser builds only ./cmd/handle-mcp.
//
//	go run ./tools/release prepare vX.Y.Z [--dry-run]
//	go run ./tools/release finish  vX.Y.Z [--dry-run]
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	var cmd, version string
	dry := false
	for _, a := range os.Args[1:] {
		switch {
		case a == "--dry-run":
			dry = true
		case cmd == "":
			cmd = a
		case version == "":
			version = a
		default:
			usage()
		}
	}
	if (cmd != "prepare" && cmd != "finish") || version == "" {
		usage()
	}
	e := &Env{R: execRunner{}, Out: os.Stderr, Root: ".", DryRun: dry, Sleep: time.Sleep}
	var err error
	if cmd == "prepare" {
		err = e.prepare(version)
	} else {
		err = e.finish(version)
	}
	if err != nil {
		e.logf("error: %v", err)
		if len(e.done) > 0 {
			e.logf("already done:")
			for _, d := range e.done {
				e.logf("  - %s", d)
			}
		} else {
			e.logf("nothing was changed")
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/release (prepare|finish) vX.Y.Z [--dry-run]")
	os.Exit(2)
}
