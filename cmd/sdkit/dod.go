package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/plan"
)

func init() { cli.Register("dod", runDod) }

func runDod(args []string) int {
	sub, rest := "", args
	if len(args) > 0 {
		sub, rest = args[0], args[1:]
	}
	if sub != "check" && sub != "accept" && sub != "accepted" && sub != "acceptances" {
		fmt.Fprintln(os.Stderr, "usage: sdkit dod check <proposal.md> | accept <proposal.md> D<n> --by <who> --reason <why> | accepted <proposal.md> D<n> | acceptances <proposal.md>")
		return cli.ExitUsage
	}
	if len(rest) == 0 {
		fmt.Fprintf(os.Stderr, "usage: sdkit dod %s <proposal.md>\n", sub)
		return cli.ExitUsage
	}
	pf := rest[0]
	b, err := os.ReadFile(pf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage: sdkit dod %s <proposal.md>%s ...\n", sub, map[bool]string{true: " D<n>"}[sub != "check" && sub != "acceptances"])
		return cli.ExitUsage
	}
	af := filepath.Dir(pf) + "/acceptances.jsonl"
	switch sub {
	case "check":
		out, ok := plan.CheckDoD(string(b))
		for _, l := range out {
			fmt.Println(l)
		}
		if !ok {
			return cli.ExitFail
		}
		return cli.ExitOK
	case "acceptances":
		if a, err := os.ReadFile(af); err == nil {
			os.Stdout.Write(a)
		}
		return cli.ExitOK
	}
	id := ""
	if len(rest) > 1 {
		id = rest[1]
	}
	if !regexp.MustCompile(`^D[0-9]`).MatchString(id) {
		fmt.Fprintf(os.Stderr, "dod-check: %s needs a DoD id like D3\n", sub)
		return cli.ExitUsage
	}
	line := plan.Item(string(b), id)
	if line == "" {
		fmt.Fprintf(os.Stderr, "dod-check: %s is not in %s\n", id, pf)
		return cli.ExitFail
	}
	if sub == "accepted" {
		if plan.Accepted(af, id, line) {
			return cli.ExitOK
		}
		return cli.ExitFail
	}
	var by, why string
	for i := 2; i < len(rest); i++ {
		switch rest[i] {
		case "--by", "--reason":
			v := ""
			if i+1 < len(rest) {
				v = rest[i+1]
			}
			if rest[i] == "--by" {
				by = v
			} else {
				why = v
			}
			i++
		default:
			fmt.Fprintf(os.Stderr, "dod-check: unknown option %s\n", rest[i])
			return cli.ExitUsage
		}
	}
	if strings.TrimSpace(by) == "" || strings.TrimSpace(why) == "" {
		fmt.Fprintln(os.Stderr, "dod-check: accept needs --by \"<who accepts>\" and --reason \"<why>\" (nothing is accepted by default)")
		return cli.ExitUsage
	}
	af, err = plan.Accept(pf, id, line, by, why)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dod-check:", strings.TrimPrefix(err.Error(), plan.ErrNotAcceptable.Error()+": "))
		return cli.ExitFail
	}
	fmt.Printf("dod-check: %s accepted by %s (recorded in %s); this binds the item as written now\n", id, by, af)
	return cli.ExitOK
}
