package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/wecii/sdkit/internal/capped"
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("capped", runCapped) }

const cappedUsage = `Run a noisy command and keep only the exit code, the failure lines and the tail.
  sdkit capped [--lines N] [--slug S] -- <command> [args...]   (one quoted argument runs through the shell)`

func runCapped(args []string) int {
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		root = wd
	}
	s, p, _ := project.Load(root)
	lines, _ := strconv.Atoi(s.String("CAPPED_LINES"))
	slug := ""
	for len(args) > 0 {
		switch args[0] {
		case "--lines", "--slug":
			v := ""
			if len(args) > 1 {
				v = args[1]
			}
			if args[0] == "--lines" {
				lines, _ = strconv.Atoi(v)
			} else {
				slug = v
			}
			args = args[min(2, len(args)):]
			continue
		case "--":
			args = args[1:]
		}
		break
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cappedUsage)
		return cli.ExitUsage
	}
	out, rc, err := capped.Run(context.Background(), capped.Options{Root: root, KitDir: p.KitDir, Slug: slug, Lines: lines, Args: args})
	fmt.Print(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
	}
	return rc
}
