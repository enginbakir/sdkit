// Package runrole runs one role as a separate headless process of any supported host: independent by construction (fresh
// context, only the handoff), pinned to a directory, read-only when the role is, and free to use another host or model
// family than the main session. At high risk it registers the spawn and records the verdict itself (trust "spawned").
//
// Exit: the host CLI's status; 127 its CLI is not installed; 2 usage, policy or config; 3 budget; 4 the answer breaks the
// contract, 5 it is valid but partial|blocked|failed (0 never means verified); 6 an evidence role's answer could not be
// recorded as a verdict; 7 and 8 only with ROUTING=on (reused valid evidence, declined to run: never a pass). A prompt is
// never logged.
package runrole

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/adapter"
	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/workflow"
)

const usage = `Usage: sdkit run-role <role> --handoff FILE [--cwd DIR] [--host H] [--tier T] [--model M] [--out FILE] [--envelope] [--ignore-budget] [--rerun ID] [--dry-run]
  role      a subagent role (analyst scout engineer developer test-writer devsecops reviewer security-auditor verifier verifier-deep)
  --host    claude|codex|opencode|cursor|gemini|ext:<adapter>. Default: VERIFIER_HOST for verifier roles, then RUN_ROLE_HOST (sdkit.env)
  --tier    light|standard|strong (small|medium|large accepted): replaces the role's tier; verifier/reviewer/security-auditor never go below their own
  --model   host-specific model id; wins over --tier (default RUN_ROLE_MODEL_<TIER>, with --tier also <HOST>_MODEL_<TIER> from core/tiers.env)
  --out     also write the answer there (it is always printed)
  --envelope  print a compact ENVELOPE/1 view of a long answer; the full answer is stored`

var contractRe = regexp.MustCompile(`(?m)^[ \t]*CONTRACT:`)
var idRe = regexp.MustCompile(`(?m)^[ \t]*ID:[ \t]*(\S*)`)

type opts struct {
	role, handoff, cwd, host, model, tier, out, rerun string
	dry, noBudget, envelope                           bool
}

type run struct {
	opts
	o, e                     io.Writer
	top, base, src, tierName string
	p                        project.Project
	get                      func(string) string
	ro, contracted           bool
	cid, rid, xid            string
	closed                   bool
	sig                      atomic.Int32
	rdur                     int64
	hcost, tq                string
	tin, tout                int
}

func rank(t string) int {
	switch t {
	case "light":
		return 1
	case "standard":
		return 2
	case "strong":
		return 3
	}
	return 0
}

func evidenceRole(b string) bool {
	return b == "verifier" || b == "reviewer" || b == "security-auditor"
}

// frontmatter reads key from the first --- block and the body after it (the awk of 1.x: every later --- line is dropped too).
func frontmatter(text string) (fm map[string]string, body string) {
	fm, f := map[string]string{}, 0
	var b []string
	for _, l := range strings.Split(text, "\n") {
		switch {
		case l == "---":
			f++
		case f == 1:
			if k, v, ok := strings.Cut(l, ":"); ok {
				if _, seen := fm[k]; !seen {
					fm[k] = strings.TrimLeft(v, " ")
				}
			}
		case f >= 2:
			b = append(b, l)
		}
	}
	return fm, strings.TrimRight(strings.Join(b, "\n"), "\n")
}

// shq quotes like bash printf %q, for the dry-run line.
func shq(s string) string {
	if s == "" {
		return "''"
	}
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(" \t\n!\"#$&'()*;<>?[\\]^`{|}~", c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (r *run) tele(typ string, f ...string) {
	if r.get("TELEMETRY") != "1" {
		return
	}
	defer func() { _ = recover() }()
	sdd.Tel.Emit(typ, f...)
}

func (r *run) logUsage(u Usage) {
	if Seams.Budget != nil {
		Seams.Budget.Log(u)
	}
}

// closeRoute closes the routing decision once with the real exit status.
func (r *run) closeRoute(rc, prc int, stderr string) {
	if r.closed || r.rid == "" || r.dry || Seams.Router == nil {
		return
	}
	r.closed = true
	Seams.Router.Outcome(r.rid, Outcome{RC: rc, PRC: prc, Stderr: stderr, DurMS: r.rdur, CostEst: r.hcost, Tokens: r.tq, In: r.tin, Out: r.tout})
}

func (r *run) fail(code int, f string, a ...any) int {
	fmt.Fprintf(r.e, f+"\n", a...)
	return code
}

// Run is `sdkit run-role`.
func Run(args []string, out, errw io.Writer) int {
	r := &run{o: out, e: errw, tq: "unavailable"}
	if len(args) == 0 {
		fmt.Fprintln(errw, usage)
		return 2
	}
	r.role, args = args[0], args[1:]
	r.envelope = os.Getenv("RESULT_ENVELOPE") == "1"
	for i := 0; i < len(args); i++ {
		val := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--handoff":
			r.handoff = val()
		case "--cwd":
			r.cwd = val()
		case "--host":
			r.host = val()
		case "--tier":
			r.tier = val()
		case "--model":
			r.model = val()
		case "--out":
			r.out = val()
		case "--rerun":
			r.rerun = val()
		case "--ignore-budget":
			r.noBudget = true
		case "--dry-run":
			r.dry = true
		case "--envelope":
			r.envelope = true
		default:
			return r.fail(2, "unknown option %s", args[i])
		}
	}
	if st, err := os.Stat(r.handoff); r.role == "" || r.handoff == "" || err != nil || st.IsDir() {
		fmt.Fprintln(errw, usage)
		return 2
	}
	if r.cwd == "" {
		r.cwd, _ = os.Getwd()
	}
	if st, err := os.Stat(r.cwd); err != nil || !st.IsDir() {
		return r.fail(2, "--cwd %s is not a directory", r.cwd)
	}
	r.cwd, _ = filepath.Abs(r.cwd)
	r.handoff, _ = filepath.Abs(r.handoff)
	r.top = r.cwd
	if t, err := gitx.Top(r.cwd); err == nil && t != "" {
		r.top = t
	}
	set, p, _ := project.Load(r.top)
	r.p, r.get = p, base.Getter(set.Raw)
	r.xid = fmt.Sprintf("rr-%d-%d", time.Now().Unix(), os.Getpid())
	return r.main()
}

func (r *run) main() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !r.dry { // a signal stops the host (its whole process tree, through ctx) and ends this run with 130 or 143
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		go func() {
			if s, ok := <-sig; ok {
				r.sig.Store(map[bool]int32{true: 130, false: 143}[s == os.Interrupt])
				cancel()
			}
		}()
	}
	if r.noBudget && !r.dry && adapter.Audit != nil {
		_ = adapter.Audit("budget.override", "role="+r.role, "source=run-role")
	}
	// the role text: the kit's, then the project's
	r.base = strings.TrimSuffix(r.role, "-deep")
	text := ""
	if b, err := kit.FS.ReadFile("core/roles/" + r.base + ".md"); err == nil {
		text = string(b)
	} else if b, err := os.ReadFile(filepath.Join(r.p.Dir(), "roles", r.base+".md")); err == nil {
		text = string(b)
	} else {
		return r.fail(2, "no role text for '%s' (looked in core/roles and %s/roles)", r.base, r.p.KitDir)
	}
	fm, body := frontmatter(text)
	tier := fm["tier"]
	if r.role != r.base {
		tier = fm["deep"]
	}
	if tier == "" {
		tier = "standard"
	}
	tier = r.roleTier(tier)
	r.ro = fm["access"] == "read-only"
	want := r.tier
	flagHost, flagModel, flagTier := r.host, r.model, r.tier
	if want != "" {
		if a, ok := map[string]string{"small": "light", "medium": "standard", "large": "strong"}[want]; ok {
			want = a
		}
		if rank(want) == 0 {
			return r.fail(2, "--tier must be light|standard|strong (or small|medium|large), got '%s'", r.tier)
		}
		if !evidenceRole(r.base) || rank(want) > rank(tier) {
			tier = want
		}
	}
	// routing (P4-08): off unless ROUTING=on
	var dec Decision
	switch routing := r.get("ROUTING"); routing {
	case "on":
		if Seams.Router == nil {
			return r.fail(2, "run-role: ROUTING=on but routing is not built into this sdkit (P4-08); refusing to run without the routing policy")
		}
		q := Query{Role: r.role, Handoff: r.handoff, Host: flagHost, Model: flagModel, Tier: flagTier, Rerun: r.rerun, IgnoreBudget: r.noBudget, Reserve: !r.dry, PID: os.Getpid(), Root: r.top}
		d, drc, err := Seams.Router.Decide(q)
		if err != nil || d.Action == "" {
			return r.fail(2, "run-role: routing produced no decision (route exit %d); refusing to run without it", drc)
		}
		dec = d
		switch d.Action {
		case "execute", "retry", "escalate":
		default:
			notes := ""
			if len(d.Notes) > 0 {
				notes = ": " + strings.Join(d.Notes, "; ")
			}
			fmt.Fprintf(r.e, "run-role: routing: %s (%s)%s\n", d.Action, d.Reason, notes)
			if d.Action == "reuse" {
				fmt.Fprintln(r.e, "run-role: exit 7 = NOT A FAILURE: valid evidence for this diff already exists, nothing was run and nothing should be retried (read it: sdd evidence)")
			}
			if d.Action == "stop" && drc == 8 {
				fmt.Fprintln(r.e, "run-role: exit 8 = NOT RUN: routing declined to start it and no valid result exists for this request; this is not a verification and not a pass")
			}
			if d.Reason == "BUDGET_EXHAUSTED" {
				fmt.Fprintf(r.e, "run-role: task state is unchanged; raise the limit in %s/sdkit.local.env, or ask the user and re-run with --ignore-budget\n", r.p.KitDir)
				r.tele("failure", "category", "budget", "role", r.role, "source", "run-role")
			}
			return drc
		}
		r.host, r.model, r.tier, r.rid = d.Adapter, d.Model, d.Tier, d.ID
		tier = d.Tier
		var w []string
		for _, x := range d.Warnings {
			w = append(w, " ["+strings.SplitN(x, ":", 2)[0]+"]")
		}
		m := r.model
		if m == "" {
			m = "default model"
		}
		fmt.Fprintf(r.e, "run-role: routing: %s %s/%s tier=%s reason=%s (%s)%s\n", d.Action, r.host, m, tier, d.Reason, d.ID, strings.Join(w, ""))
	case "off", "":
	default:
		return r.fail(2, "run-role: ROUTING must be on or off (is '%s'); refusing to guess whether the routing policy applies", routing)
	}
	r.tierName = tier
	effort := dec.Effort
	if r.rid == "" { // routing off: host, model and effort are resolved here
		if r.host == "" {
			if evidenceRole(r.base) {
				r.host = r.get("VERIFIER_HOST")
			}
			if r.host == "" {
				r.host = r.get("RUN_ROLE_HOST")
			}
		}
		if r.host == "" {
			return r.fail(2, "no host: pass --host or set RUN_ROLE_HOST (VERIFIER_HOST for independent roles) in %s/%s", r.p.KitDir, r.p.EnvFile)
		}
		TU := strings.ToUpper(tier)
		if r.model == "" {
			r.model = r.get("RUN_ROLE_MODEL_" + TU)
		}
		tiers := r.tiersEnv()
		HU := strings.ToUpper(r.host)
		if r.model == "" && want != "" {
			r.model = tiers.String(HU + "_MODEL_" + TU)
		}
		if r.host == "codex" && want != "" {
			effort = tiers.String("CODEX_EFFORT_" + TU)
			if effort == "" {
				effort = map[string]string{"light": "low", "standard": "medium"}[tier]
				if effort == "" {
					effort = "high"
				}
			}
		}
	}
	// a contracted handoff
	hb, _ := os.ReadFile(r.handoff)
	hs := string(hb)
	creply := ""
	if r.contracted = contractRe.MatchString(hs); r.contracted {
		if m := idRe.FindStringSubmatch(hs); m != nil {
			r.cid = m[1]
		}
		creply = " Then end with the single HANDOFF-RESULT line the handoff asks for."
	} else if r.top != "" && state.ReadOpen(r.p).Profile == "high-assurance" && workflow.Contracts("high-assurance", r.get("CONTRACTS") == "1", r.role) == "required" {
		fmt.Fprintf(r.e, "run-role: profile high-assurance requires a contract for role %s: add CONTRACT: 1, ID: and REPLY-END: to the handoff (references/contract.md).\n", r.role)
		if r.mode() == "strict" {
			return 2
		}
	}
	prompt := fmt.Sprintf("You are running as the \"%s\" role of the spec-driven-kit workflow. Your project directory (holds sdkit.env) is `%s/`.\nYour working directory is %s. Do not touch files outside it.\n\n%s\n\n# Handoff\n\n%s\n\nAnswer in exactly the format your role defines.%s",
		r.role, r.p.KitDir, r.cwd, body, strings.TrimRight(hs, "\n"), creply)

	tmp, err := os.MkdirTemp("", "sdkit-rr-")
	if err != nil {
		return r.fail(2, "run-role: cannot make a temporary directory: %v", err)
	}
	defer os.RemoveAll(tmp)
	lastMsg, promptFile := filepath.Join(tmp, "last.txt"), filepath.Join(tmp, "prompt.txt")
	finder := adapter.Finder{ProjectDir: r.p.Dir()}
	var cmd adapter.Cmd
	internal := false // ext:litellm without a file runs in this process
	if strings.HasPrefix(r.host, "ext:") {
		xname := strings.TrimPrefix(r.host, "ext:")
		path, ok := finder.Find(r.host)
		if !ok && r.host != "ext:litellm" {
			return r.fail(2, "run-role: no executable adapter '%s' (adapters.d/%s.sh)", xname, xname)
		}
		_ = os.WriteFile(promptFile, []byte(prompt), 0o600)
		if !r.dry && adapter.Audit != nil {
			_ = adapter.Audit("adapter.exec", "adapter="+xname, "role="+r.role, "bytes="+strconv.Itoa(utf8.RuneCountInString(prompt)))
		}
		cmd = adapter.Cmd{CLI: path, Prompt: -1, Args: []string{"execute", "--role", r.role, "--cwd", r.cwd, "--tier", tier, "--ro", map[bool]string{true: "1", false: "0"}[r.ro]}}
		if !ok {
			cmd.CLI, internal = r.host, true
		}
		if r.model != "" {
			cmd.Args = append(cmd.Args, "--model", r.model)
		}
		cmd.Args = append(cmd.Args, "--prompt-file", promptFile)
	} else {
		c, ok := adapter.Build(r.host, adapter.Req{Role: r.role, Model: r.model, Effort: effort, Cwd: r.cwd, Prompt: prompt, LastMsg: lastMsg, RO: r.ro})
		if !ok {
			return r.fail(2, "unknown host '%s' (claude|codex|opencode|cursor|gemini|ext:<adapter>)", r.host)
		}
		cmd = c
	}
	if r.dry {
		if r.contracted && Seams.Contract != nil {
			m, _ := Seams.Contract.Issue(r.handoff, r.role, true)
			fmt.Fprintln(r.e, m)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "(cd %s && %s", shq(r.cwd), cmd.CLI)
		for i, a := range cmd.Args {
			if i == cmd.Prompt {
				fmt.Fprintf(&b, " <prompt:%d chars>", utf8.RuneCountInString(a))
			} else {
				b.WriteString(" " + shq(a))
			}
		}
		fmt.Fprintln(r.o, b.String()+")")
		return 0
	}
	if !internal && !strings.HasPrefix(r.host, "ext:") {
		if _, err := proc.LookPath(cmd.CLI); err != nil {
			r.tele("failure", "category", "host-missing", "role", r.role, "host", r.host, "source", "run-role")
			r.closeRoute(127, 0, "")
			return r.fail(127, "run-role: '%s' is not installed (host %s)", cmd.CLI, r.host)
		}
	}
	return r.execute(ctx, cmd, internal, prompt, tier, effort, dec, lastMsg, creply)
}

func (r *run) mode() string {
	if m := r.get("SDD_MODE"); m != "" {
		return m
	}
	return "strict"
}

// roleTier is AGENT_<ROLE>_TIER of the settings; an independent role is never moved below its own tier; a deep role keeps its tier.
func (r *run) roleTier(own string) string {
	if r.role != r.base {
		return own
	}
	v := r.get("AGENT_" + strings.ToUpper(strings.ReplaceAll(r.base, "-", "_")) + "_TIER")
	if rank(v) == 0 || (evidenceRole(r.base) && rank(v) < rank(own)) {
		return own
	}
	return v
}

// tiersEnv is core/tiers.env, else the project's tiers.env.
func (r *run) tiersEnv() *envfile.Settings {
	s := envfile.New()
	if b, err := kit.FS.ReadFile("core/tiers.env"); err == nil {
		s.LoadBytes("tiers.env", b)
	} else if b, err := os.ReadFile(filepath.Join(r.p.Dir(), "tiers.env")); err == nil {
		s.LoadBytes("tiers.env", b)
	}
	return s
}

func tail(s string, n int) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}
