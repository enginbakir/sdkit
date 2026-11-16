package main

import (
	"fmt"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/guard"
)

func init() { cli.Register("guard", runGuard) }

// runGuard is `sdkit guard pre-commit | pre-push [remote] [url]`, called by the git-hook shims; pre-push reads the refs on stdin.
func runGuard(args []string) int {
	cwd, _ := os.Getwd()
	root, err := gitx.Top(cwd)
	if err != nil || len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sdkit guard pre-commit|pre-push (inside a git repository)")
		return cli.ExitUsage
	}
	switch args[0] {
	case "pre-commit":
		return guard.PreCommit(root, os.Stderr)
	case "pre-push":
		return guard.PrePush(root, os.Stdin, os.Stderr)
	}
	fmt.Fprintln(os.Stderr, "usage: sdkit guard pre-commit|pre-push")
	return cli.ExitUsage
}
