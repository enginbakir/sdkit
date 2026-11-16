package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/preflight"
)

func init() { cli.Register("preflight", runPreflight) }

// runPreflight: sdkit preflight spec-publish <id> [--offline] | tasks <tasks.json> [--offline]
// Reads and checks only; prints every cause at once (OK/WARN/FAIL) and exits 1 when something would fail.
func runPreflight(args []string) int {
	usage := func() int {
		fmt.Fprintln(os.Stderr, "usage: sdkit preflight spec-publish <id> [--offline] | tasks <tasks.json> [--offline]")
		return cli.ExitUsage
	}
	if len(args) < 2 {
		return usage()
	}
	offline := false
	for _, a := range args[2:] {
		if a != "--offline" {
			return usage()
		}
		offline = true
	}
	wd, _ := os.Getwd()
	root := wd
	if t, err := gitx.Top(wd); err == nil {
		root = t
	}
	self, _ := os.Executable()
	o := preflight.Opts{Root: root, Offline: offline, Getenv: os.Getenv, Self: func(a ...string) (string, bool) {
		var b bytes.Buffer
		c := exec.Command(self, a...)
		c.Stdout, c.Stderr = &b, &b
		return strings.TrimSpace(b.String()), c.Run() == nil
	}}
	var ls []preflight.Line
	switch args[0] {
	case "spec-publish":
		ls = preflight.SpecPublish(o, args[1])
	case "tasks":
		ls = preflight.Tasks(o, args[1])
	default:
		return usage()
	}
	for _, l := range ls {
		fmt.Println(l)
	}
	if preflight.Failed(ls) {
		return cli.ExitFail
	}
	return cli.ExitOK
}
