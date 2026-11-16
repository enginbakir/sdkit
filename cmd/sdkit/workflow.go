package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/workflow"
)

func init() { cli.Register("workflow", runWorkflow) }

const workflowUsage = `workflow policy [--json] | contracts [--role R] | gate <green|verified> | require --role R --boundary agent|process --marker yes|no | announce | profiles | evidence-roles | capabilities [--json]`

// Pending lists the agents still in flight for a task; internal/contract supplies it when that exists.
var Pending func(task string) []string

func runWorkflow(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, workflowUsage)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		cwd, _ := os.Getwd()
		if r, err := gitx.Top(cwd); err == nil {
			root = r
		} else {
			root = cwd
		}
	}
	set, p, _ := project.Load(root)
	open := state.ReadOpen(p)
	flag := func(name string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == name {
				return args[i+1]
			}
		}
		return ""
	}
	level := func(role string) string { return workflow.Contracts(open.Profile, set.Bool("CONTRACTS"), role) }
	tele := func(f ...string) { // a workflow.decision event for the open task; off unless TELEMETRY=1, never changes the answer
		if open.Task != "" {
			f = append(f, "task", open.Task)
		}
		sdd.Tel.Emit("workflow.decision", f...)
	}
	json1 := len(args) > 0 && args[0] == "--json"
	switch cmd {
	case "announce":
		if lvl := level("developer"); lvl != "off" {
			tele("source", "contract-policy", "outcome", lvl, "profile", open.Profile)
		}
	case "require":
		role := flag("--role")
		if flag("--marker") == "yes" || level(role) != "required" {
			return cli.ExitOK
		}
		tele("source", "contract-policy", "outcome", "missing-marker", "profile", open.Profile, "role", role[strings.LastIndex(role, ":")+1:], "reason", "missing-field", "note", "boundary="+flag("--boundary"))
		fmt.Printf("profile %s requires a contract for role %s: add CONTRACT: 1, ID: and REPLY-END: to the handoff (references/contract.md).\n", open.Profile, role[strings.LastIndex(role, ":")+1:])
		return cli.ExitFail
	case "contracts":
		fmt.Println(level(flag("--role")))
	case "gate":
		if len(args) == 0 || (args[0] != "green" && args[0] != "verified") || open.Task == "" || level("developer") != "required" || Pending == nil {
			return cli.ExitOK
		}
		if pend := strings.Join(Pending(open.Task), " "); pend != "" {
			fmt.Printf("agents still in flight or interrupted: %s. Wait for them, or close an interrupted one (sdkit contract close <id> --reason interrupted) and retry it once (sdkit state %s retry <id>).\n", pend, open.Task)
			return cli.ExitFail
		}
	case "evidence-roles":
		if r := workflow.EvidenceRoles(open.Risk); len(r) > 0 {
			fmt.Println(strings.Join(r, " "))
		}
	case "capabilities":
		caps := workflow.Capabilities()
		if json1 {
			printJSON(caps)
			break
		}
		for _, c := range caps {
			fmt.Printf("%-52s contract=%s | parse=%s | identity=%s | record=%s | stale=%s | gate=%s | failure=%s\n", c.Path, c.SpawnContract, c.VerdictParse, c.TrustedIdentity, c.EvidenceRecord, c.StaleDetection, c.CompletionGate, c.FailureReport)
		}
	case "policy":
		cd, cs := level("developer"), level("scout")
		ver := workflow.Verifier(open.Risk, open.Profile)
		if json1 {
			type lvls struct {
				Writing string `json:"writing_and_deciding_roles"`
				Lookup  string `json:"lookup_roles"`
			}
			printJSON(struct {
				Task      string   `json:"task"`
				Risk      string   `json:"risk"`
				Profile   string   `json:"profile"`
				Verifier  string   `json:"verifier"`
				Contracts lvls     `json:"contracts"`
				Retries   int      `json:"automatic_retries"`
				Evidence  []string `json:"evidence_roles"`
			}{open.Task, open.Risk, open.Profile, ver, lvls{cd, cs}, 1, workflow.EvidenceRoles(open.Risk)})
			break
		}
		dash := func(s string) string {
			if s == "" {
				return "-"
			}
			return s
		}
		fmt.Printf("task=%s risk=%s profile=%s verifier=%s contracts=%s (scout/analyst: %s) automatic-retries=1\n", dash(open.Task), dash(open.Risk), open.Profile, ver, cd, cs)
	case "profiles":
		fmt.Println(strings.Join(workflow.ProfileLines, "\n"))
	default:
		fmt.Fprintln(os.Stderr, workflowUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

// printJSON prints v as jq does: two-space indent, keys in the order given (struct order; maps are sorted).
func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
