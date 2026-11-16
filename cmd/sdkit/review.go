package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/lint"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/review"
)

func init() { cli.Register("review", runReview) }

const reviewUsage = "usage: sdkit review plan <artifact> [--round N] [--json] | status <id> | changed <id> | settle <id> <topic> --why Y [--who W] | settled <id> [--prompt] | raise-cap <id> --to N --why Y [--who W] | hash <id> | begin <id> [--final] | record <id> --verdict V --isolation I [--findings F] [--set full|cheap] [--roles N] | check <id> | lint <id> [--target T] | accept <id> <finding-id> [--who W] --why Y"

func runReview(args []string) int {
	usage := func() int { fmt.Fprintln(os.Stderr, reviewUsage); return cli.ExitUsage }
	if len(args) == 0 {
		return usage()
	}
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		root = wd
	}
	c := review.Load(root)
	cmd, a := args[0], args[1:]
	arg := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	// flags parses --name value pairs (bool names take none); false on an unknown flag.
	flags := func(from int, vals map[string]*string, bools map[string]*bool) bool {
		for i := from; i < len(a); i++ {
			if p, ok := vals[a[i]]; ok {
				*p = arg(i + 1)
				i++
			} else if p, ok := bools[a[i]]; ok {
				*p = true
			} else {
				return false
			}
		}
		return true
	}
	done := func(msg string, err error) int {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			if e, ok := review.IsError(err); ok {
				return e.Code
			}
			return cli.ExitUsage
		}
		fmt.Println(msg)
		return cli.ExitOK
	}
	id := arg(0)
	switch cmd {
	case "plan":
		js, rn := false, ""
		if !review.OkName(id) || !flags(1, map[string]*string{"--round": &rn}, map[string]*bool{"--json": &js}) {
			return usage()
		}
		n, _ := strconv.Atoi(rn)
		r, err := c.PlanRound(id, n)
		if err != nil {
			return done("", err)
		}
		if js {
			fmt.Print(r.JSON())
		} else {
			fmt.Print(r.Text())
		}
		return cli.ExitOK
	case "hash", "begin":
		var tgt string
		final := false
		if !review.OkName(id) || !flags(1, map[string]*string{"--target": &tgt}, map[string]*bool{"--final": &final}) || (final && cmd == "hash") {
			return usage()
		}
		if cmd == "begin" {
			return done(c.Begin(id, tgt, final))
		}
		t, err := c.Target(id, tgt)
		if err == nil {
			t, err = review.Hash(t)
		}
		return done(t, err)
	case "record":
		var v, iso, ff, set, roles string
		if !review.OkName(id) || !flags(1, map[string]*string{"--verdict": &v, "--isolation": &iso, "--findings": &ff, "--set": &set, "--roles": &roles}, nil) {
			return usage()
		}
		nr, _ := strconv.Atoi(roles)
		return done(c.Record(id, v, iso, ff, review.RecordOpt{Set: set, Roles: nr}))
	case "status":
		if !review.OkName(id) {
			return usage()
		}
		fmt.Println(c.Status(id))
		return cli.ExitOK
	case "changed":
		if !review.OkName(id) {
			return usage()
		}
		l, same, err := c.Changed(id)
		if err != nil {
			return done("", err)
		}
		for _, x := range l {
			fmt.Println(x)
		}
		fmt.Printf("review: %d file(s) to review, %d unchanged since the last evidence (their earlier result stands)\n", len(l), same)
		return cli.ExitOK
	case "settle":
		var why, who string
		if !review.OkName(id) || arg(1) == "" || !flags(2, map[string]*string{"--why": &why, "--who": &who}, nil) {
			return usage()
		}
		return done(c.Settle(id, arg(1), why, who))
	case "settled":
		pr := false
		if !review.OkName(id) || !flags(1, nil, map[string]*bool{"--prompt": &pr}) {
			return usage()
		}
		if pr {
			fmt.Print(c.SettledPrompt(id))
		} else {
			for _, l := range c.Settled(id) {
				fmt.Println(l)
			}
		}
		return cli.ExitOK
	case "raise-cap":
		var to, why, who string
		if !review.OkName(id) || !flags(1, map[string]*string{"--to": &to, "--why": &why, "--who": &who}, nil) {
			return usage()
		}
		nto, _ := strconv.Atoi(to)
		return done(c.RaiseCap(id, nto, why, who))
	case "lint":
		var tgt string
		if !review.OkName(id) || !flags(1, map[string]*string{"--target": &tgt}, nil) {
			return usage()
		}
		ps, err := c.LintTarget(id, tgt)
		if err != nil {
			return done("", err)
		}
		for _, p := range ps {
			fmt.Println(p)
		}
		if n := lint.Errors(ps); n > 0 {
			fmt.Printf("lint: %d error(s), %d warning(s)\n", n, len(ps)-n)
			return cli.ExitFail
		}
		fmt.Printf("lint: clean (%d warning(s))\n", len(ps))
		return cli.ExitOK
	case "check":
		if !review.OkName(id) {
			return usage()
		}
		r := c.Check(id)
		fmt.Println("review: " + r.Reason)
		if !r.OK() {
			return cli.ExitFail
		}
		return cli.ExitOK
	case "accept":
		var who, why string
		if !review.OkName(id) || arg(1) == "" || !flags(2, map[string]*string{"--who": &who, "--why": &why}, nil) {
			return usage()
		}
		return done(c.Accept(id, arg(1), who, why))
	}
	return usage()
}
