package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/pr"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("pr", runPR) }

const prUsage = `usage: sdkit pr open [--repo-dir D] [--check|--reuse|--update|--close BRANCH] "<title>" <description-file> [destination-branch]
       sdkit pr check [--base REF] [--list] [--milestone]`

func runPR(args []string) int {
	if len(args) == 0 || (args[0] != "open" && args[0] != "check") {
		fmt.Fprintln(os.Stderr, prUsage)
		return cli.ExitUsage
	}
	sub, args := args[0], args[1:]
	dir, _ := os.Getwd()
	end := func(err error) int {
		var e *pr.Error
		switch {
		case err == nil:
			return cli.ExitOK
		case errors.As(err, &e):
			if e.Msg != "" {
				fmt.Fprintln(os.Stderr, e.Msg)
			}
			return e.Code
		}
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitFail
	}
	if sub == "check" {
		o := pr.CheckOptions{Dir: dir, Out: os.Stdout, Tool: prTool(dir)}
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--base":
				if i++; i >= len(args) {
					fmt.Fprintln(os.Stderr, prUsage)
					return cli.ExitUsage
				}
				o.Base = args[i]
			case "--list":
				o.List = true
			case "--milestone":
				o.Milestone = true
			default:
				fmt.Fprintln(os.Stderr, prUsage)
				return cli.ExitUsage
			}
		}
		n, err := pr.Check(context.Background(), o)
		if err != nil {
			return end(err)
		}
		if n > 0 {
			return cli.ExitFail
		}
		return cli.ExitOK
	}
	r := &pr.Request{Mode: "create", Out: os.Stdout, Err: os.Stderr}
	check := false
	var pos []string
	for i := 0; i < len(args); i++ {
		val := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--repo-dir":
			if dir = val(); dir == "" {
				fmt.Fprintln(os.Stderr, "--repo-dir needs a directory")
				return cli.ExitUsage
			}
		case "--reuse":
			r.Reuse = true
		case "--check":
			check = true
		case "--update":
			r.Mode = "update"
		case "--close":
			if r.CloseBranch = val(); r.CloseBranch == "" {
				fmt.Fprintln(os.Stderr, "--close needs a branch")
				return cli.ExitUsage
			}
			r.Mode = "close"
		default:
			pos = append(pos, args[i])
		}
	}
	top, err := gitx.Top(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot enter %s\n", dir)
		return cli.ExitUsage
	}
	s, _, _ := project.Load(top)
	r.Dir, r.Getenv = top, func(k string) string {
		if v := os.Getenv(k); v != "" || secret(k) {
			return v
		}
		return s.String(k)
	}
	r.Host = r.Getenv("PR_HOST")
	if v, ok := s.Raw("PR_HOST"); r.Host == "" && ok {
		r.Host = v
	}
	if check {
		n, err := r.Check(context.Background())
		if err != nil {
			return end(err)
		}
		if n > 0 {
			fmt.Fprintln(os.Stderr, "Fill in the items above, then run the same command again. Nothing was committed or pushed.")
			return cli.ExitUsage
		}
		fmt.Fprintln(os.Stderr, "PR host ready.")
		return cli.ExitOK
	}
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, prUsage)
		return cli.ExitUsage
	}
	r.Title, r.DescFile, r.Dest = pos[0], pos[1], r.Getenv("BASE_BRANCH")
	if len(pos) > 2 {
		r.Dest = pos[2]
	}
	if r.Dest == "" {
		r.Dest = "develop"
	}
	return end(r.Run(context.Background()))
}

// secret keys are read from the environment only, never from sdkit.env.
func secret(k string) bool { return k == "BITBUCKET_TOKEN" || k == "BITBUCKET_EMAIL" }

// prTool finds a helper of `pr check`: a script of the project's kit, else a sdkit command of that name.
func prTool(dir string) func(string) []string {
	return func(name string) []string {
		top, _ := gitx.Top(dir)
		sub := map[string]string{"bench-history": "bench/history.sh"}[name]
		for _, p := range []string{sub, "scripts/" + name + ".sh", "bin/" + name + ".sh"} {
			if p == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(top, p)); err == nil {
				return []string{"bash", p}
			}
		}
		for _, n := range cli.Names() {
			if n == name {
				if self, err := exec.LookPath(os.Args[0]); err == nil {
					return []string{self, name}
				}
			}
		}
		return nil
	}
}
