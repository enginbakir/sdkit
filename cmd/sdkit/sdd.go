package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() {
	cli.Register("sdd", runSDD)
	verdictRecorder = sdd.Recorder{Env: func() *sdd.Env { return sdd.New(sdd.Root()) }}
	sdd.PendingAgents = func(task string) []string { // Pending is set by internal/contract's wiring, after this init
		if Pending == nil {
			return nil
		}
		return Pending(task)
	}
}

const sddUsage = `sdd start <slug> --risk low|medium|high --proposal <proposal.md> [--profile small|standard|high-assurance]
sdd advance <phase> [--test-cmd CMD] | status | phase | task | mode | check-fresh | fingerprint | abort [failed|blocked|partial]
sdd transitions | evidence [--json [--with-fingerprint]] | policy [--json] | floor [--validate]
sdd gate edit <path> | gate push | ci-check
managed (SDD_MANAGED=on): start --ticket KEY | block --spec-change --reason R | revalidate --sha NEW | evidence-sync [--pr URL] | done --merged`

func runSDD(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, sddUsage)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	if cmd == "policy" {
		return runWorkflow(append([]string{"policy"}, args...))
	}
	e := sdd.New(sdd.Root())
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	switch cmd {
	case "start":
		return e.Start(args)
	case "advance":
		testCmd := ""
		for i := 1; i < len(args); i++ {
			if args[i] != "--test-cmd" {
				if e.Phase() == "" {
					return e.Advance("", "")
				}
				return e.Die("unknown option " + args[i])
			}
			testCmd = arg(i + 1)
			i++
		}
		return e.Advance(arg(0), testCmd)
	case "status":
		e.Status()
	case "transitions":
		e.Transitions()
	case "phase":
		if p := e.Phase(); p != "" {
			fmt.Println(p)
		}
	case "task":
		if t := e.TaskSlug(); t != "" {
			fmt.Println(t)
		}
	case "mode":
		fmt.Println(e.Mode())
	case "check-fresh":
		return e.CheckFresh()
	case "fingerprint":
		fp, err := e.Fingerprint()
		if err != nil {
			return e.Die(err.Error())
		}
		fmt.Println(fp)
	case "gate":
		if k := arg(0); e.Mode() != "off" && k != "edit" && k != "push" {
			return e.Die("usage: gate edit <path> | gate push")
		}
		if ok, why := e.Gate(arg(0), arg(1)); !ok {
			fmt.Println(why)
			return cli.ExitFail
		}
	case "floor":
		return e.Floor(arg(0) == "--validate")
	case "ci-check":
		return e.CICheck()
	case "abort":
		e.Abort(arg(0))
	case "evidence":
		return e.ShowEvidence(args)
	case "_spawn":
		return e.Spawn(arg(0), args[min(1, len(args)):])
	case "_spawn-close":
		return e.SpawnClose(args)
	case "_evidence", "_verdict":
		return runEvidence(e, cmd, args)
	case "block", "revalidate", "evidence-sync", "done":
		if !e.Managed() {
			fmt.Fprintln(os.Stderr, sddUsage)
			return cli.ExitUsage
		}
		if cmd == "done" && arg(0) != "--merged" {
			return e.Die("usage: done --merged (the merge is checked, never declared: the PR head must be in the resolved base)")
		}
		return e.ManagedCommand(cmd, args)
	default:
		fmt.Fprintln(os.Stderr, sddUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

// runEvidence: _evidence <role> <PASS|BLOCK> [--source S] [--run ID] [--task SLUG]; _verdict <PASS|BLOCK> ... = the verifier's. Hooks and run-role only.
func runEvidence(e *sdd.Env, cmd string, args []string) int {
	if e.Phase() == "" {
		if cmd == "_verdict" {
			return e.Die("no active task. Start one: sdkit sdd start <slug> --risk <low|medium|high> --proposal <path>")
		}
		fmt.Fprintln(os.Stderr, "no active task")
		return cli.ExitContract
	}
	role := "verifier"
	if cmd == "_evidence" {
		role, args = firstArg(args), rest(args)
	}
	result := firstArg(args)
	args = rest(args)
	var src, run, task string
	for i := 0; i < len(args); i++ {
		v := ""
		if i+1 < len(args) {
			v = args[i+1]
		}
		switch args[i] {
		case "--source":
			src = v
		case "--run":
			run = v
		case "--task":
			task = v
		default:
			return e.Die("unknown option " + args[i])
		}
		i++
	}
	msg, code := e.Evidence(role, result, src, run, task)
	switch {
	case code == 4:
		fmt.Fprintln(os.Stderr, msg)
	case code == 1 && !strings.HasPrefix(msg, "evidence refused"):
		return e.Die(msg)
	case cmd == "_verdict" && code == 0:
	default:
		fmt.Println(msg)
	}
	return code
}

func firstArg(a []string) string {
	if len(a) > 0 {
		return a[0]
	}
	return ""
}

func rest(a []string) []string {
	if len(a) > 0 {
		return a[1:]
	}
	return nil
}
