package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/runrole"
	"github.com/wecii/sdkit/internal/usage"
)

func init() {
	cli.Register("usage", runUsage)
	runrole.Seams.Budget = usageBudget{}
}

const usageHelp = `usage: sdkit usage <command>
  record --role R [--tier T] [--host H] [--model M] [--provider P] [--in N] [--out N] [--cache-read N] [--cache-write N] [--slug S] [--id ID]
         [--session SID] [--origin sdk|observed] [--kind snapshot|final] [--status S] [--source X] [--tq actual|unavailable] [--cost-est USD] [--force]
  report [slug] [--project] [--session ID|current] [--as-model MODEL] [--json]
  budget [slug]            used vs TOKEN_BUDGET_TASK: exit 0, 3 from TOKEN_BUDGET_WARN_PCT (default 70), 4 at 100%
  scan [--session ID | --since YYYY-MM-DD] [--dir DIR] [--slug S]     Claude Code transcripts, logged as observed
  context [transcript] [--top N] [--json]                             where a Claude Code session's context came from
  prices [--json] | extract <transcript> [--since TS] | reconcile [--slug S] [--dir DIR] | live show [--role R] [--slug S] [--json] | window`

// usageBudget is the Budget seam of run-role (P3-08): Check counts the spawn and names the limits reached BEFORE the spawn starts, Log stores usage.
type usageBudget struct{}

func usageEnv() (string, project.Project, *envfile.Settings) {
	wd, _ := os.Getwd()
	top, err := project.Root(wd)
	if err != nil {
		top = wd
	}
	s, p, _ := project.Load(top)
	return top, p, s
}

func (usageBudget) Check(slug string) string {
	_, p, s := usageEnv()
	return usage.Check(p, s, slug)
}

func (usageBudget) Log(u runrole.Usage) {
	_, p, s := usageEnv()
	if s.String("USAGE_LOG") != "1" && s.String("USAGE_LINE") != "1" {
		return
	}
	l := usage.Line{V: 2, TS: usage.Now(), Role: u.Role, Tier: u.Tier, Host: u.Host, Model: u.Model, ID: u.ID, Origin: "sdk", Kind: "final", Source: "run-role",
		In: usage.Num(u.In), Out: usage.Num(u.Out), CacheRead: usage.Num(u.CacheRead), CacheWrite: usage.Num(u.CacheWrite), TQ: u.TQ, Status: u.Status}
	if c, err := strconv.ParseFloat(u.CostEst, 64); err == nil && costRe.MatchString(u.CostEst) {
		l.CostEst, l.CostSrc = &c, "host-estimate"
	}
	slug := usage.CurrentSlug(p)
	_ = os.MkdirAll(filepath.Join(p.Dir(), "work", slug), 0o755)
	_ = usage.Log(usage.LedgerFile(p, slug), l)
}

var costRe = regexp.MustCompile(`^[0-9]+([.][0-9]+)?$`)

func usageErr(f string, a ...any) int {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	return cli.ExitUsage
}

func runUsage(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usageHelp)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	top, p, s := usageEnv()
	pricesFile := os.Getenv("TELEMETRY_PRICES")
	if pricesFile == "" {
		pricesFile = filepath.Join(p.Dir(), "pricing.json")
	}
	// opts parses "--flag value" pairs, "--bool" flags and positionals
	flags := func(valued, bools []string) (map[string]string, []string, bool) {
		m, pos := map[string]string{}, []string(nil)
		for len(args) > 0 {
			a := args[0]
			args = args[1:]
			switch {
			case has(valued, a):
				if len(args) == 0 {
					return nil, nil, false
				}
				m[a], args = args[0], args[1:]
			case has(bools, a):
				m[a] = "1"
			case len(a) > 1 && a[0] == '-':
				fmt.Fprintf(os.Stderr, "unknown option %s\n", a)
				return nil, nil, false
			default:
				pos = append(pos, a)
			}
		}
		return m, pos, true
	}
	switch cmd {
	case "record", "log":
		f, _, ok := flags([]string{"--role", "--tier", "--host", "--model", "--in", "--out", "--cache-read", "--cache-write", "--slug", "--id", "--source", "--tq",
			"--cost-est", "--session", "--provider", "--origin", "--kind", "--status"}, []string{"--force"})
		if !ok {
			return cli.ExitUsage
		}
		if s.String("USAGE_LOG") != "1" && s.String("USAGE_LINE") != "1" && f["--force"] == "" {
			return cli.ExitOK
		}
		if f["--role"] == "" {
			return usageErr("--role is required")
		}
		if o := orDef(f["--origin"], "sdk"); o != "sdk" && o != "observed" {
			return usageErr("--origin must be sdk or observed")
		}
		if k := orDef(f["--kind"], "snapshot"); k != "snapshot" && k != "final" {
			return usageErr("--kind must be snapshot or final")
		}
		n := map[string]int{}
		for _, k := range []string{"--in", "--out", "--cache-read", "--cache-write"} {
			v, err := strconv.Atoi(orDef(f[k], "0"))
			if err != nil {
				return usageErr("usage: bad number in an argument")
			}
			n[k] = v
		}
		l := usage.Line{V: 2, TS: usage.Now(), Role: f["--role"], Tier: f["--tier"], Host: f["--host"], Model: f["--model"], Provider: f["--provider"], Session: f["--session"],
			ID: f["--id"], Origin: orDef(f["--origin"], "sdk"), Kind: orDef(f["--kind"], "snapshot"), Source: f["--source"], In: usage.Num(n["--in"]), Out: usage.Num(n["--out"]),
			CacheRead: usage.Num(n["--cache-read"]), CacheWrite: usage.Num(n["--cache-write"]), TQ: f["--tq"], Status: f["--status"]}
		if c, err := strconv.ParseFloat(f["--cost-est"], 64); err == nil && costRe.MatchString(f["--cost-est"]) {
			l.CostEst, l.CostSrc = &c, "host-estimate"
		}
		slug := orDef(f["--slug"], usage.CurrentSlug(p))
		_ = os.MkdirAll(filepath.Join(p.Dir(), "work", slug), 0o755)
		if err := usage.Log(usage.LedgerFile(p, slug), l); err != nil {
			fmt.Fprintln(os.Stderr, "usage:", err)
			return cli.ExitFail
		}
	case "report":
		f, pos, ok := flags([]string{"--session", "--as-model"}, []string{"--project", "--json"})
		if !ok {
			return cli.ExitUsage
		}
		slug := ""
		if len(pos) > 0 {
			slug = pos[len(pos)-1]
		}
		work := filepath.Join(p.Dir(), "work")
		var files []string
		scope := "every open task"
		if slug != "" {
			files, scope = []string{filepath.Join(work, slug, "usage.jsonl")}, "task "+slug
		} else {
			files, _ = filepath.Glob(filepath.Join(work, "*", "usage.jsonl"))
		}
		if f["--project"] != "" {
			if slug != "" {
				files, scope = append(files, filepath.Join(work, "_archive", slug+".usage.jsonl")), "task "+slug+" (with archive)"
			} else {
				a, _ := filepath.Glob(filepath.Join(work, "_archive", "*.usage.jsonl"))
				files, scope = append(files, a...), "project: open and archived tasks"
			}
		}
		sess := f["--session"]
		if sess == "current" {
			sess = ""
			ts := ""
			for _, fl := range files {
				for _, l := range usage.ReadLines(fl) {
					if l.Session != "" && l.TS >= ts {
						sess, ts = l.Session, l.TS
					}
				}
			}
			if sess == "" {
				fmt.Fprintln(os.Stderr, "report: no ledger line carries a session id yet")
				return cli.ExitOK
			}
			scope += ", session " + sess + " (the one with the newest record)"
		} else if sess != "" {
			scope += ", session " + sess
		}
		o := usage.Options{Scope: scope, Prices: pricesFile, Session: sess, AsModel: f["--as-model"]}
		if slug != "" {
			o.Spawns = usage.Spawns(p, slug)
		}
		r := usage.Compute(files, o)
		switch {
		case r == nil && f["--json"] != "":
			fmt.Println(`{"empty":true}`)
		case r == nil:
			fmt.Println("no usage recorded (set USAGE_LOG=1 in sdkit.env)")
		case f["--json"] != "":
			b, _ := json.MarshalIndent(r, "", "  ")
			fmt.Println(string(b))
		default:
			fmt.Print(r.Text())
		}
	case "budget":
		slug := ""
		if len(args) > 0 {
			slug = args[0]
		}
		slug = orDef(slug, usage.CurrentSlug(p))
		b := usage.TokenBudget(p, s, slug)
		if !b.Set {
			return cli.ExitOK
		}
		fmt.Println(b.Line(slug))
		return b.Level
	case "prices":
		out, ok := usage.PricesStatus(pricesFile, len(args) > 0 && args[0] == "--json")
		fmt.Println(out)
		if !ok {
			return cli.ExitFail
		}
	case "scan":
		f, _, ok := flags([]string{"--session", "--since", "--dir", "--slug"}, nil)
		if !ok {
			return cli.ExitUsage
		}
		dir := orDef(f["--dir"], os.Getenv("CLAUDE_TRANSCRIPT_DIR"))
		n, slug, err := usage.Scan(p, top, usage.ScanOptions{Session: f["--session"], Since: f["--since"], Dir: dir, Slug: f["--slug"]})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitFail
		}
		fmt.Printf("scan: %d ledger line(s) written to %s/work/%s/usage.jsonl (origin observed)\n", n, filepath.Base(p.Dir()), slug)
	case "context":
		f, pos, ok := flags([]string{"--top"}, []string{"--json"})
		if !ok {
			return cli.ExitUsage
		}
		topn, err := strconv.Atoi(orDef(f["--top"], "5"))
		if err != nil || topn < 0 {
			topn = 5
		}
		tf := ""
		if len(pos) > 0 {
			tf = pos[len(pos)-1]
		} else if l, e := usage.LatestTranscript(usage.TranscriptDir(top)); e == nil {
			tf = l
		}
		c, err := usage.Context(tf, topn)
		if tf == "" || err != nil {
			fmt.Fprintln(os.Stderr, "context: no readable transcript (Claude Code only; pass a path)")
			return cli.ExitFail
		}
		if f["--json"] != "" {
			b, _ := json.Marshal(c)
			fmt.Println(string(b))
		} else {
			fmt.Print(c.Text())
		}
	case "extract":
		f, pos, ok := flags([]string{"--since"}, nil)
		if !ok || len(pos) == 0 {
			return usageErr("usage: sdkit usage extract <transcript> [--since TS]")
		}
		t, err := usage.Extract(pos[0], f["--since"])
		if err != nil {
			return usageErr("usage: sdkit usage extract <transcript> [--since TS]")
		}
		b, _ := json.Marshal(t)
		fmt.Println(string(b))
	case "reconcile":
		f, _, ok := flags([]string{"--slug", "--dir"}, nil)
		if !ok {
			return cli.ExitUsage
		}
		fmt.Println(usage.Reconcile(p, top, orDef(f["--dir"], os.Getenv("CLAUDE_TRANSCRIPT_DIR")), f["--slug"]))
	case "window":
		if w := usage.Window(p, usage.CurrentSlug(p)); w != "" {
			fmt.Println(w)
		}
	case "live":
		if len(args) == 0 || args[0] != "show" {
			return usageErr("usage: sdkit usage live show [--role R] [--slug S] [--json]")
		}
		args = args[1:]
		f, _, ok := flags([]string{"--role", "--slug"}, []string{"--json", "--line"})
		if !ok {
			return cli.ExitUsage
		}
		file := usage.LedgerFile(p, orDef(f["--slug"], usage.CurrentSlug(p)))
		if _, err := os.Stat(file); err != nil {
			if f["--json"] != "" {
				fmt.Println(`{"runs":0}`)
			} else {
				fmt.Println("usage not available (nothing recorded yet)")
			}
			return cli.ExitOK
		}
		role, all := usage.LiveTotals(file, f["--role"]), usage.LiveTotals(file, "")
		switch {
		case f["--json"] != "":
			b, _ := json.Marshal(map[string]usage.Live{"role": role, "task": all})
			fmt.Println(string(b))
		case f["--role"] != "":
			fmt.Printf("%s: %s\n", f["--role"], role.Fragment())
		default:
			fmt.Printf("orchestrator: %s | task total: %s\n", usage.LiveTotals(file, "orchestrator").Fragment(), all.Fragment())
		}
	default:
		fmt.Fprintln(os.Stderr, usageHelp)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

func has(l []string, s string) bool {

	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func orDef(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
