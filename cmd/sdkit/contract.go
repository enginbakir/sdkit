package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/contract"
	"github.com/wecii/sdkit/internal/sdd"
)

const contractUsage = `sdkit contract issue FILE|- [--role R] [--task S] [--check-only] [--source X]
sdkit contract check-result FILE|- [--id ID] [--role R] [--task S] [--cwd DIR] [--record] [--legacy-ok] [--source X]
sdkit contract close ID [--reason interrupted] [--task S] | pending [--role R] [--task S] | status [slug] | id FILE|-
exit: 0 valid (check-result: status done), 1 valid but partial|blocked|failed, 2 invalid. Exit 0 is not "verified".`

func init() {
	cli.Register("contract", runContract)
	Pending = func(task string) []string { return contract.New(sdd.Root()).Pending("", task) }
}

func runContract(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, contractUsage)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	c := contract.New(sdd.Root())
	var o contract.Options
	var arg, reason string
	for i := 0; i < len(args); i++ {
		val := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--role":
			o.Role = val()
		case "--task":
			o.Task = val()
		case "--cwd":
			o.Cwd = val()
		case "--id":
			o.WantID = val()
		case "--check-only":
			o.CheckOnly = true
		case "--record":
			o.Record = true
		case "--legacy-ok":
			o.LegacyOK = true
		case "--source":
			o.Source = val()
		case "--reason":
			reason = val()
		default:
			if arg == "" {
				arg = args[i]
			}
		}
	}
	read := func(f string) (string, bool) {
		var b []byte
		var err error
		if f == "-" {
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(f)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "contract: cannot read %s\n", f)
		}
		return strings.TrimRight(string(b), "\n"), err == nil
	}
	switch cmd {
	case "issue", "check-result":
		if arg == "" {
			fmt.Fprintln(os.Stderr, contractUsage)
			return cli.ExitUsage
		}
		text, ok := read(arg)
		if !ok {
			return cli.ExitUsage
		}
		stage := "handoff"
		if cmd == "check-result" {
			stage = "result"
		}
		r := c.Check(stage, text, o)
		if l := r.Line(); l != "" {
			fmt.Println(l)
		}
		return r.Exit()
	case "id":
		if arg == "" {
			arg = "-"
		}
		text, ok := read(arg)
		if !ok {
			return cli.ExitUsage
		}
		fmt.Println(contract.ID(text))
	case "pending":
		for _, id := range c.Pending(o.Role, o.Task) {
			fmt.Println(id)
		}
	case "close":
		if reason == "" {
			reason = "interrupted"
		}
		if !strings.Contains(arg, "/") {
			arg = c.Active(o.Task) + "/" + arg
		}
		if st, ok := c.Close(arg, reason); ok {
			fmt.Printf("CONTRACT=closed id=%s state=%s\n", arg, st)
		} else {
			fmt.Fprintf(os.Stderr, "contract: %s was never issued or is not a <task>/<label> id\n", arg)
			return cli.ExitUsage
		}
	case "status":
		s := arg
		if s == "" {
			s = c.Active("")
		}
		if s == "" {
			fmt.Println("no open task")
			return cli.ExitOK
		}
		ls := c.Status(s)
		if len(ls) == 0 {
			fmt.Println("no contracts for " + s)
		}
		for _, l := range ls {
			fmt.Println(l)
		}
	default:
		fmt.Fprintln(os.Stderr, contractUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}
