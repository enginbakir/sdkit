package main

import (
	"fmt"
	"io"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/lookup"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

func init() { cli.Register("lookup", runLookup) }

// taskDir resolves the repository and the project directory; ok is false outside a git repository.
func taskDir() (string, string, bool) {
	cwd, _ := os.Getwd()
	top, err := gitx.Top(cwd)
	if err != nil {
		return "", "", false
	}
	return top, project.Resolve(top).KitDir, true
}

// runLookup is `sdkit lookup get|put|clear <slug> [question]`: scout answers kept for one code state.
func runLookup(args []string) int {
	if len(args) < 2 || !state.ValidSlug(args[1]) {
		fmt.Fprintln(os.Stderr, "usage: sdkit lookup get|put|clear <slug> [question]; slug may contain letters, digits, _ . - only")
		return cli.ExitUsage
	}
	top, kd, ok := taskDir()
	if !ok {
		fmt.Fprintln(os.Stderr, "sdkit lookup needs a git repository")
		return cli.ExitFail
	}
	c := lookup.C{Root: top, KitDir: kd, Slug: args[1]}
	q := ""
	if len(args) > 2 {
		q = args[2]
	}
	switch args[0] {
	case "get":
		if a, ok := c.Get(q); ok {
			fmt.Print(a)
			return cli.ExitOK
		}
		return cli.ExitFail
	case "put":
		if q == "" {
			return cli.ExitUsage
		}
		b, _ := io.ReadAll(os.Stdin)
		if c.Put(q, string(b)) != nil {
			return cli.ExitFail
		}
	case "clear":
		c.Clear()
	default:
		return cli.ExitUsage
	}
	return cli.ExitOK
}
