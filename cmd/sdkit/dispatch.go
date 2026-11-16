package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/dispatch"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

func init() { cli.Register("dispatch", runDispatch) }

const dispatchUsage = "usage: sdkit dispatch claim <task-id> <tasks.md> | done <task-id> | release <task-id> | status  [--slug S]"

// Exit: 0 ok, 2 usage, 3 refused (reason printed), 4 parallelism is off.
func runDispatch(args []string) int {
	sub, slug := "", ""
	var pos []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--slug" && i+1 < len(args):
			slug = args[i+1]
			i++
		case sub == "":
			sub = args[i]
		default:
			pos = append(pos, args[i])
		}
	}
	need := map[string]int{"claim": 2, "done": 1, "release": 1, "status": 0}
	if n, ok := need[sub]; !ok || len(pos) < n || (sub != "claim" && len(pos) > n) {
		fmt.Fprintln(os.Stderr, dispatchUsage)
		return cli.ExitUsage
	}
	wd, _ := os.Getwd()
	top, err := project.Root(wd)
	if err != nil {
		top = wd
	}
	s, p, _ := project.Load(top)
	if slug == "" {
		slug = state.ActiveSlug(p)
	}
	if slug == "" {
		slug = "_misc"
	}
	if !state.ValidSlug(slug) {
		fmt.Fprintln(os.Stderr, "dispatch: invalid slug", slug)
		return cli.ExitUsage
	}
	l := dispatch.Ledger{Path: filepath.Join(p.Dir(), "work", slug, "dispatch.jsonl"), Max: s.Int("MAX_PARALLEL", 3),
		Head: func() string { h := gitx.RevParse(top, "HEAD"); return h[:min(7, len(h))] }}
	var msg string
	switch sub {
	case "claim":
		if v, ok := s.Lookup("PARALLEL"); ok && v != "on" && v != "" {
			fmt.Fprintln(os.Stderr, "parallel dispatch is off (PARALLEL=off in the settings); run tasks in order")
			return cli.ExitContract
		}
		b, rerr := os.ReadFile(pos[1])
		if rerr != nil {
			fmt.Fprintln(os.Stderr, dispatchUsage)
			return cli.ExitUsage
		}
		msg, err = l.Claim(pos[0], string(b))
	case "done", "release":
		msg, err = l.Finish(pos[0], sub == "release")
	default:
		b, _ := os.ReadFile(l.Path)
		fmt.Println(strings.Join(l.Status(b), "\n"))
		return cli.ExitOK
	}
	var r dispatch.Refused
	if errors.As(err, &r) {
		fmt.Println("refused: " + r.Error())
		return cli.ExitBudget
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dispatch:", err)
		return cli.ExitFail
	}
	fmt.Println(msg)
	return cli.ExitOK
}
