package runrole

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/adapter"
	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/verdict"
)

// openTask is what run-role reads of <project-dir>/.sdd/state.json.
func (r *run) openTask() (path string, t struct{ Proposal, Risk string }, ok bool) {
	path = filepath.Join(r.p.Dir(), ".sdd", "state.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return path, t, false
	}
	json.Unmarshal(b, &t)
	return path, t, true
}

func (r *run) sddEnv() *sdd.Env {
	e := sdd.New(r.top)
	e.Out, e.Err = nopWriter{}, nopWriter{}
	return e
}

type nopWriter struct{}

func (nopWriter) Write(b []byte) (int, error) { return len(b), nil }

// execute is everything after the command line is known: gates, registration, the process, the answer, the records.
func (r *run) execute(ctx context.Context, cmd adapter.Cmd, internal bool, prompt, tier, effort string, dec Decision, lastMsg, creply string) int {
	_, task, hasTask := r.openTask()
	propPath := ""
	if task.Proposal != "" {
		if st, err := os.Stat(filepath.Join(r.top, task.Proposal)); err == nil && !st.IsDir() {
			propPath = filepath.Join(r.top, task.Proposal)
		}
	}
	// F08: a verifier cannot judge a DoD that has no method, a bare "none" or a duplicate id; say so before it is paid for
	if r.base == "verifier" && propPath != "" {
		b, _ := os.ReadFile(propPath)
		if out, ok := plan.CheckDoD(string(b)); !ok {
			fmt.Fprintf(r.e, "run-role: DoD not verifiable, fix %s first (nothing was run):\n", task.Proposal)
			for _, l := range out {
				fmt.Fprintf(r.e, "  %s\n", l)
			}
			r.tele("failure", "category", "gate", "role", r.role, "reason", "no-dod", "source", "run-role")
			r.closeRoute(2, 0, "")
			return 2
		}
	}
	// a contracted handoff is validated and registered before anything is spent
	if r.contracted {
		if Seams.Contract == nil {
			fmt.Fprintln(r.e, "run-role: warning: contracts are not wired into this build; the handoff is not validated and the answer is not checked")
		} else if m, ok := Seams.Contract.Issue(r.handoff, r.role, false); !ok {
			fmt.Fprintf(r.e, "run-role: %s\n", m)
			r.closeRoute(2, 0, "")
			return 2
		}
	}
	// spawn and token budget: the spawn point on hosts without the Agent-tool hook (with routing on, the router did this)
	if r.rid == "" && (r.get("MAX_SPAWNS") != "" || r.get("TOKEN_BUDGET_TASK") != "") && r.mode() != "off" && Seams.Budget != nil {
		if slug := state.ActiveSlug(r.p); slug != "" {
			if over := Seams.Budget.Check(slug); over != "" {
				fmt.Fprintf(r.e, "run-role: budget reached for task %s: %s\n", slug, over)
				if r.mode() == "strict" && !r.noBudget {
					fmt.Fprintln(r.e, "run-role: ask the user, then re-run with --ignore-budget")
					r.tele("failure", "category", "budget", "role", r.role, "host", r.host, "source", "run-role")
					return 3
				}
			}
		}
	}
	// evidence roles: register this run (the diff it will see) before it starts. Reviewer and security-auditor are evidence
	// roles only at high risk; at lower risk their answers keep their free form.
	evrun := ""
	if evidenceRole(r.base) && r.mode() != "off" && hasTask && (r.base == "verifier" || task.Risk == "high") {
		evrun = fmt.Sprintf("rr-%d-%d-%d", time.Now().Unix(), os.Getpid(), rand.Intn(32768))
		if r.sddEnv().Spawn(r.base, []string{"--source", "run-role", "--run", evrun}) != 0 {
			evrun = ""
		}
	}
	span, slug := "", ""
	if r.get("TELEMETRY") == "1" {
		slug, span = state.ActiveSlug(r.p), r.xid
	}
	id := r.xid
	t0 := time.Now()
	r.tele("agent.start", "span", span, "role", r.role, "host", r.host, "model", r.model, "tier", tier, "source", "run-role")
	if dec.WaitS > 0 && os.Getenv("SDKIT_ROUTE_NO_SLEEP") != "1" {
		fmt.Fprintf(r.e, "run-role: routing: waiting %ds before this retry (backoff)\n", dec.WaitS)
		select {
		case <-time.After(time.Duration(dec.WaitS) * time.Second):
		case <-ctx.Done():
		}
	}
	// The child gets SDKIT_CHILD_RUN: this run is accounted HERE. A Claude Code child session would otherwise log the same
	// tokens a second time through its own Stop hook.
	var raw, errOut string
	var rc int
	if ctx.Err() == nil {
		runCtx := ctx
		if dec.TimeoutS > 0 {
			var c context.CancelFunc
			runCtx, c = context.WithTimeout(ctx, time.Duration(dec.TimeoutS)*time.Second)
			defer c()
		}
		raw, errOut, rc = r.launch(runCtx, cmd, internal, id)
		if runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			rc = 124
		}
	}
	dur := time.Since(t0).Milliseconds()
	interrupted := func(code int) {
		r.logUsage(Usage{Role: r.role, Tier: tier, Host: r.host, Model: r.model, ID: id, TQ: "unavailable", Status: "interrupted"})
		if r.contracted && Seams.Contract != nil {
			Seams.Contract.Close(r.cid, "interrupted")
		}
		if evrun != "" {
			r.sddEnv().SpawnClose([]string{"--run", evrun, "--reason", "interrupted"})
		}
		r.rdur = dur
		r.closeRoute(code, 0, errOut)
	}
	if s := int(r.sig.Load()); s != 0 { // stopped by a signal: tokens may have been spent but none were read
		interrupted(s)
		return s
	}
	if rc != 0 {
		fmt.Fprintf(r.e, "run-role: %s exited %d\n", cmd.CLI, rc)
		if errOut != "" {
			fmt.Fprintln(r.e, tail(errOut, 20))
		}
		r.tele("agent.end", "span", span, "role", r.role, "host", r.host, "model", r.model, "tier", tier, "outcome", "error", "dur-ms", strconv.FormatInt(dur, 10), "q", "duration=actual", "source", "run-role")
		r.tele("failure", "category", "host-error", "role", r.role, "host", r.host, "source", "run-role", "note", "exit "+strconv.Itoa(rc))
		interrupted(rc)
		return rc
	}

	// the answer, and token usage where the host prints it
	var last []byte
	if r.host == "codex" {
		last, _ = os.ReadFile(lastMsg)
	}
	a := adapter.Parse(r.host, []byte(raw), last)
	r.tin, r.tout, r.hcost = a.In, a.Out, a.Cost
	if a.Tokens {
		r.tq = "actual"
	}
	cache := r.host == "claude" || strings.HasPrefix(r.host, "ext:")
	if Seams.Envelope != nil && r.envelope {
		sl := slug
		if sl == "" {
			sl = state.ActiveSlug(r.p)
		}
		if sl == "" {
			sl = "_none"
		}
		if r.out == "" {
			r.out = filepath.Join(r.p.Dir(), "work", sl, "out", r.role+"-"+r.xid+".md")
		}
		os.MkdirAll(filepath.Dir(r.out), 0o755)
		os.WriteFile(r.out, []byte(a.Text+"\n"), 0o644)
		fmt.Fprintln(r.o, Seams.Envelope(a.Text, r.role, r.out))
	} else {
		fmt.Fprintln(r.o, a.Text)
	}
	if r.out != "" {
		os.MkdirAll(filepath.Dir(r.out), 0o755)
		os.WriteFile(r.out, []byte(a.Text+"\n"), 0o644)
	}
	ds := strconv.FormatInt(dur, 10)
	end := []string{"span", span, "role", r.role, "host", r.host, "model", r.model, "tier", tier, "outcome", "ok", "dur-ms", ds, "q", "duration=actual"}
	if a.Tokens {
		end = append(end, "in", strconv.Itoa(a.In), "out", strconv.Itoa(a.Out))
		if cache { // cache counts exist only on claude; gemini has none: leave them out rather than write 0
			end = append(end, "cache-read", strconv.Itoa(a.CacheRead), "cache-write", strconv.Itoa(a.CacheWrite))
		}
		end = append(end, "q", "tokens=actual")
	} else {
		end = append(end, "q", "tokens=unavailable")
	}
	r.tele("agent.end", append(end, "source", "run-role")...)
	r.logUsage(Usage{Role: r.role, Tier: tier, Host: r.host, Model: r.model, ID: id, TQ: r.tq, In: a.In, Out: a.Out, CacheRead: a.CacheRead, CacheWrite: a.CacheWrite, CostEst: a.Cost})
	if r.get("USAGE_LINE") == "1" {
		r.usageLine(a, cache)
	}
	erc, prc := 0, 0
	r.rdur = dur
	if evrun != "" {
		line, c := r.record(a.Text, evrun, propPath)
		prc = c
		fmt.Fprintf(r.e, "run-role: %s\n", line)
		if prc == 2 {
			why := "malformed"
			if strings.Contains(line, "MALFORMED[semantic]") {
				why = "inconsistent"
			}
			r.tele("failure", "category", "verdict-malformed", "reason", why, "role", r.role, "source", "run-role")
		}
		if prc == 2 || prc == 3 {
			erc = 6
			r.sddEnv().SpawnClose([]string{"--run", evrun, "--reason", "unrecorded"})
		}
	}
	if r.contracted && Seams.Contract != nil {
		msg, crc := Seams.Contract.Result(a.Text, r.cid, r.role, r.cwd)
		fmt.Fprintf(r.e, "run-role: %s\n", msg)
		switch crc {
		case 0:
		case 1:
			r.closeRoute(5, prc, "")
			return 5
		default:
			r.closeRoute(4, prc, "")
			return 4
		}
	}
	r.closeRoute(erc, prc, "")
	return erc
}

// launch starts the host (or the adapter) and waits. The host's whole process tree dies with ctx.
func (r *run) launch(ctx context.Context, cmd adapter.Cmd, internal bool, id string) (stdout, stderr string, rc int) {
	env := []string{"SDKIT_CHILD_RUN=" + id}
	if internal {
		pf := cmd.Args[len(cmd.Args)-1]
		b, _ := os.ReadFile(pf)
		out, e := adapter.LiteLLM(ctx, r.get, r.role, r.model, r.ro, string(b))
		if e != nil {
			return "", e.Msg, e.Code
		}
		return string(out), "", 0
	}
	name, args := cmd.CLI, cmd.Args
	if strings.HasPrefix(r.host, "ext:") {
		n, pre := adapter.ExtCommand(cmd.CLI)
		name, args = n, append(pre, args...)
	}
	res, err := proc.Run(ctx, r.cwd, env, name, args...)
	if err != nil && res.Code <= 0 {
		return res.Stdout, res.Stderr + err.Error(), 1
	}
	return res.Stdout, res.Stderr, res.Code
}

// record parses the answer as a verdict and records it with this run's registration (trust "spawned"). The line is what the
// parser printed; the code is its exit status: 0 PASS, 1 BLOCK, 2 malformed, 3 well formed but refused.
func (r *run) record(answer, evrun, propPath string) (line string, code int) {
	o := verdict.Options{Role: r.base, NoNormalize: os.Getenv("PARSE_NORMALIZE") == "0"}
	if r.base == "verifier" {
		o.ProposalPath = propPath
	}
	o.Accepted = func(proposal, id string) bool {
		b, err := os.ReadFile(proposal)
		if err != nil {
			return false
		}
		l := plan.Item(string(b), id)
		return l != "" && plan.Accepted(filepath.Join(filepath.Dir(proposal), "acceptances.jsonl"), id, l)
	}
	res := verdict.ParseWith(answer, o)
	line, code = res.Line(), res.Code()
	if res.Kind != "" {
		return
	}
	role := res.Role
	if role == "" {
		role = r.base
	}
	rec := sdd.Recorder{Env: r.sddEnv}
	if msg, refused := rec.Record(role, res.Verdict, "run-role", evrun, ""); refused {
		line += fmt.Sprintf("\n%s well-formed, NOT recorded: %s", res.Verdict, msg)
		code = verdict.CodeRefused
	}
	return
}

// usageLine is the one-line summary the Claude SubagentStop hook prints; on stderr, so the answer stays clean.
func (r *run) usageLine(a adapter.Answer, cache bool) {
	if !a.Tokens {
		fmt.Fprintf(r.e, "[sdkit] %s done — token usage not available (%s gave no data)\n", r.role, r.host)
		return
	}
	k := func(n int) string {
		if n >= 1000 {
			return fmt.Sprintf("%.1fk", float64(n)/1000)
		}
		return strconv.Itoa(n)
	}
	t, c, extra := a.In+a.CacheRead+a.CacheWrite, "n/a", ""
	if cache && t > 0 {
		c = fmt.Sprintf("%d%%", a.CacheRead*100/t)
		extra = fmt.Sprintf(" (cache read %s, write %s)", k(a.CacheRead), k(a.CacheWrite))
	}
	fmt.Fprintf(r.e, "[sdkit] %s done — in: %s%s, out: %s, cache hit: %s\n", r.role, k(t), extra, k(a.Out), c)
}
