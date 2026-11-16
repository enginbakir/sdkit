package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/setup"
)

func init() { cli.Register("sync", runSync) }

func runSync(args []string) int {
	root, _ := os.Getwd()
	check, force := false, false
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--force":
			force = true
		default:
			p, err := filepath.Abs(a)
			if err != nil || a == "" || a[0] == '-' {
				fmt.Fprintln(os.Stderr, "usage: sdkit sync [--check] [--force] [dir]")
				return cli.ExitUsage
			}
			root = p
		}
	}
	s := setup.New(root)
	if k := setup.ResolveKit(root); k != "" {
		s.KitDir = k
	}
	s.Force = force
	ok, err := s.Sync(check)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit sync:", err)
		return cli.ExitFail
	}
	if !ok {
		return cli.ExitFail
	}
	return cli.ExitOK
}
