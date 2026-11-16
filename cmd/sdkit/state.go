package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

func init() { cli.Register("state", runState) }

const stateUsage = `usage: sdkit state <slug> init [risk] [branch] | show | get <key> | set <key> <value> | task <id> <status> | retry <id> | spawn | path`

func runState(args []string) int {
	if len(args) < 2 || args[0] == "" || args[1] == "" {
		fmt.Fprintln(os.Stderr, stateUsage)
		return cli.ExitUsage
	}
	slug, cmd, a := args[0], args[1], args[2:]
	arg := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	if !state.ValidSlug(slug) {
		fmt.Fprintln(os.Stderr, "slug may contain letters, digits, _ . - only")
		return cli.ExitUsage
	}
	cwd, _ := os.Getwd()
	top, err := gitx.Top(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit: not in a git repository")
		return cli.ExitFail
	}
	set, p, _ := project.Load(top)
	fail := func(err error) int {
		if err == state.ErrMissing {
			fmt.Fprintf(os.Stderr, "no state for '%s': run  sdkit state %s init\n", slug, slug)
		} else {
			fmt.Fprintln(os.Stderr, "sdkit:", err)
		}
		return cli.ExitFail
	}
	usage := func(msg string) int {
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		return cli.ExitUsage
	}
	path := state.Path(p, slug)
	switch cmd {
	case "path":
		fmt.Println(path)
	case "init":
		if err := state.Init(p, slug, arg(0), arg(1)); err != nil {
			return fail(err)
		}
		fmt.Println(path)
	case "show", "get":
		s, err := state.Read(p, slug)
		if err != nil {
			return fail(err)
		}
		if cmd == "show" {
			fmt.Println(string(s.Marshal()))
		} else if arg(0) == "" {
			return cli.ExitUsage
		} else {
			fmt.Println(s.Get(arg(0)))
		}
	case "set", "task":
		if arg(0) == "" || (cmd == "task" && arg(1) == "") {
			return cli.ExitUsage
		}
		err := state.Update(p, slug, func(s *state.State) error {
			if cmd == "set" {
				return s.Set(arg(0), arg(1))
			}
			return s.SetTask(arg(0), arg(1))
		})
		if err != nil && err != state.ErrMissing {
			return usage(err.Error())
		} else if err != nil {
			return fail(err)
		}
	case "retry":
		if arg(0) == "" {
			return cli.ExitUsage
		}
		used := false
		err := state.Update(p, slug, func(s *state.State) error { used = !s.UseRetry(arg(0)); return nil })
		if err != nil {
			return fail(err)
		}
		if used {
			fmt.Fprintf(os.Stderr, "retry for '%s' already used: stop and ask the user\n", arg(0))
			return cli.ExitFail
		}
	case "spawn":
		n := 0
		if err := state.Update(p, slug, func(s *state.State) error { s.Spawns++; n = s.Spawns; return nil }); err != nil {
			return fail(err)
		}
		max, _ := strconv.Atoi(set.String("MAX_SPAWNS"))
		switch {
		case max <= 0:
			fmt.Printf("spawns: %d\n", n)
		case n > max:
			fmt.Fprintf(os.Stderr, "spawn %d is over MAX_SPAWNS=%d\n", n, max)
			return cli.ExitFail
		default:
			fmt.Printf("spawns: %d of %d\n", n, max)
		}
	default:
		return usage(fmt.Sprintf("unknown command '%s'", cmd))
	}
	return cli.ExitOK
}
