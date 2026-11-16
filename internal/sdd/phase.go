package sdd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/store"
	"github.com/wecii/sdkit/internal/verify"
	"github.com/wecii/sdkit/internal/workflow"
)

func index(phase string) int {
	for i, p := range Phases {
		if p == phase {
			return i
		}
	}
	return -1
}

// Next is the only valid move from phase: the next one ("" after the last). There is no way back except abort.
func Next(phase string) string {
	if i := index(phase); i >= 0 && i+1 < len(Phases) {
		return Phases[i+1]
	}
	return ""
}

func needsVerifier(risk, profile string) bool { return risk != "low" || profile == "high-assurance" }

var (
	reHeadDoD = regexp.MustCompile(`(?i)^#+[ \t]+.*definition of done`)
	reHead    = regexp.MustCompile(`^#+[ \t]`)
	reItem    = regexp.MustCompile(`^[ \t]*([-*]|[0-9]+\.)[ \t]+[^ \t]`)
	reRace    = regexp.MustCompile(`(?i)^#+[ \t]+.*(race|ordering|timing)`)
	reTask    = regexp.MustCompile(`^\s*-\s*\[[ xX]\]\s*(\S+)`)
)

// hasDoD: a "Definition of Done" heading followed by at least one list item.
func hasDoD(text string) bool {
	on := false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case reHeadDoD.MatchString(l):
			on = true
		case on && reHead.MatchString(l):
			return false
		case on && reItem.MatchString(l):
			return true
		}
	}
	return false
}

func hasRaceSection(text string) bool {
	for _, l := range strings.Split(text, "\n") {
		if reRace.MatchString(l) {
			return true
		}
	}
	return false
}

// missingSizes names the task lines of tasks.md that carry a role: but no size: ("T1 T2 ").
func missingSizes(text string) string {
	var out string
	for _, l := range strings.Split(text, "\n") {
		if m := reTask.FindStringSubmatch(l); m != nil && strings.Contains(l, "role:") && !strings.Contains(l, "size:") {
			out += m[1] + " "
		}
	}
	return out
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// Start opens a task. The risk label is the user's, confirmed before this call.
func (e *Env) Start(args []string) int {
	if e.managed() {
		return e.Mgd.Command(e, "start", args)
	}
	slug, risk, proposal, profile := "", "", "", "standard"
	if len(args) > 0 {
		slug, args = args[0], args[1:]
	}
	for i := 0; i < len(args); i++ {
		val := ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch args[i] {
		case "--risk":
			risk = val
		case "--proposal":
			proposal = val
		case "--profile":
			profile = val
		default:
			return e.die("unknown option " + args[i])
		}
		i++
	}
	if slug == "" {
		return e.die("usage: start <slug> --risk <low|medium|high> --proposal <path>")
	}
	if !state.ValidSlug(slug) || strings.HasPrefix(slug, ".") {
		return e.die("slug '" + slug + "' may only use letters, digits, '.', '_' and '-', and may not start with '.'")
	}
	if risk != "low" && risk != "medium" && risk != "high" {
		return e.die("--risk must be low, medium or high (the user-confirmed label)")
	}
	if proposal == "" {
		return e.die("--proposal <path to the proposal.md that will hold the Definition of Done> is required")
	}
	if err := workflow.Check(profile, risk); err != nil {
		return e.die(err.Error())
	}
	open := func(t *Task) int {
		return e.die(fmt.Sprintf("task '%s' is still open (phase %s). Finish it or run: sdkit sdd abort", t.Task, t.Phase))
	}
	if t := e.load(); t != nil {
		return open(t)
	}
	if e.Mode() != "off" {
		if why := FloorValidate(e.Get("RISK_FLOOR_GLOBS")); why != "" {
			return e.die("RISK_FLOOR_GLOBS is invalid: " + why + " (expected level:glob,level:glob with level low|medium|high)")
		}
	}
	at := now()
	nt := &Task{Task: slug, Risk: risk, Profile: profile, Proposal: proposal, Phase: "intake", StartedAt: at, History: []Step{{Phase: "intake", At: at}}}
	err := store.Update(e.statePath(), func(old []byte) ([]byte, error) {
		if old != nil {
			return nil, errExists
		}
		return nt.Marshal(), nil
	})
	if err != nil {
		if o := e.load(); o != nil { // two starts at once: the second sees the first's file
			return open(o)
		}
		return e.die(err.Error())
	}
	e.emit("task.start", "task", slug, "risk", risk, "profile", profile, "phase", "intake")
	e.printf("task '%s' opened (risk=%s, profile=%s, phase=intake). Write the proposal with a Definition of Done, then: sdkit sdd advance proposed\n", slug, risk, profile)
	return 0
}

var errExists = errors.New("task exists")

// Transitions prints the valid moves and what each requires.
func (e *Env) Transitions() {
	for i := 1; i < len(Phases); i++ {
		e.printf("%-18s %s\n", Phases[i-1]+" > "+Phases[i], entryRule(Phases[i]))
	}
	if e.managed() {
		for _, p := range []string{"proposed", "verified", "done"} {
			e.printf("%-18s %s\n", "  managed: "+p, e.Mgd.Rule(p))
		}
	}
	e.printf("%-18s %s\n", "verified|pr > verified", "re-verification, only while the verified diff is stale: the same checks as verified")
	if e.managed() {
		e.printf("%-18s %s\n", "blocked > intake", "managed: sdkit sdd revalidate --sha <new approved revision>; clears .blocked, drops the old evidence, then the proposed gate again")
	}
	e.printf("%-18s %s\n", "any > (aborted)", "sdkit sdd abort; the only way back, and it ends the task")
}

func entryRule(phase string) string {
	switch phase {
	case "proposed":
		return "proposal.md has a Definition of Done list (high risk: design.md has a race/ordering/timing section)"
	case "red":
		return "--test-cmd runs and FAILS"
	case "green":
		return "the tests PASS, the risk floor holds, no agent is still in flight (high-assurance)"
	case "verified":
		return "the risk floor holds; low: the verify command passes; medium/high/high-assurance: an independent PASS for this exact diff"
	case "pr":
		return "none beyond verified: the push gate already holds the diff to the verified one before a PR exists"
	case "done":
		return "none (the PR is merged: the user's call)"
	}
	return ""
}

// Status prints one line about the open task.
func (e *Env) Status() {
	t := e.load()
	if t == nil {
		e.printf("no active task\n")
		return
	}
	s := fmt.Sprintf("task=%s risk=%s phase=%s", t.Task, t.Risk, t.Phase)
	if t.Profile != "" && t.Profile != "standard" {
		s += " profile=" + t.Profile
	}
	if t.Verdict != nil && t.Verdict.Result != "" {
		s += " verdict=" + t.Verdict.Result
	}
	if (t.Phase == "verified" || t.Phase == "pr") && !e.checkFresh(t) {
		s += " evidence=stale"
	}
	if t.Ticket != "" {
		s += fmt.Sprintf(" ticket=%s spec=%s@%.12s", t.Ticket, t.SpecID, t.SpecSha)
	}
	if t.Blocked != nil && t.Blocked.Kind != "" {
		s += fmt.Sprintf(" blocked=%s:%s", t.Blocked.Kind, t.Blocked.Ref)
	}
	e.printf("%s\n", s)
}

// Abort ends the task without finishing it; status is the terminal word of the value report (failed, blocked, partial, else aborted).
func (e *Env) Abort(status string) {
	switch status {
	case "failed", "blocked", "partial":
	default:
		status = "aborted"
	}
	if _, err := os.Stat(e.statePath()); err == nil {
		e.emit("task.end", "outcome", "aborted")
		e.finalize(status)
	}
	e.close()
	e.printf("task aborted\n")
}

func (e *Env) finalize(status string) {
	defer func() { _ = recover() }()
	e.Rep.Finalize(status)
}

func (e *Env) close() {
	os.Remove(filepath.Join(e.P.Dir(), ".frozen"))
	os.RemoveAll(e.stateDir())
}

// runTest runs the test command in the project; its output goes to .sdd/last-test.log. It returns the exit status.
func (e *Env) runTest(phase, cmd string) int {
	t0 := time.Now()
	_ = os.MkdirAll(e.stateDir(), 0o755)
	rc := e.runLogged(filepath.Join(e.stateDir(), "last-test.log"), func(c *os.File) int {
		sh := proc.Shell(context.Background(), cmd)
		sh.Dir, sh.Stdout, sh.Stderr = e.Root, c, c
		return exitCode(sh.Run())
	})
	out := "pass"
	if rc != 0 {
		out = "fail"
	}
	e.emit("test.result", "phase", phase, "outcome", out, "dur-ms", strconv.FormatInt(time.Since(t0).Milliseconds(), 10), "q", "duration=actual")
	return rc
}

func (e *Env) runLogged(log string, run func(*os.File) int) int {
	f, err := os.Create(log)
	if err != nil {
		return 1
	}
	defer f.Close()
	return run(f)
}

// runVerify runs the verify command of a low-risk task: VERIFY_CMD, else the verify engine. Output goes to .sdd/last-verify.log.
func (e *Env) runVerify() int {
	_ = os.MkdirAll(e.stateDir(), 0o755)
	return e.runLogged(filepath.Join(e.stateDir(), "last-verify.log"), func(f *os.File) int {
		if c := e.Get("VERIFY_CMD"); c != "" {
			sh := proc.Shell(context.Background(), c)
			sh.Dir, sh.Stdout, sh.Stderr = e.Root, f, f
			return exitCode(sh.Run())
		}
		return verify.Run(context.Background(), e.Root, verify.Options{}, f)
	})
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) && ee.ExitCode() > 0 {
		return ee.ExitCode()
	}
	return 127
}

// Advance moves the open task to target if its gate holds. A refused move leaves the state untouched, says why on
// stderr and is reported as phase.reject. Two advances at once: one wins.
func (e *Env) Advance(target, testCmd string) int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	r := e.advance(t, target, testCmd)
	if r == nil {
		return 0
	}
	cat := "gate"
	if strings.HasPrefix(r.Reason, "tests-") || r.Reason == "" && (target == "red" || target == "green") {
		cat = "tests"
	}
	f := []string{"phase", target, "category", cat}
	if r.Reason != "" {
		f = append(f, "reason", r.Reason)
	}
	e.emit("phase.reject", f...)
	e.die(r.Msg)
	return 1
}

func rej(reason, f string, a ...any) *Refusal { return &Refusal{reason, fmt.Sprintf(f, a...)} }

func (e *Env) advance(t *Task, target, testCmd string) *Refusal {
	cur, risk, profile, seq := t.Phase, t.Risk, t.Profile, t.EvSeq
	if profile == "" {
		profile = "standard" // a task opened before profiles
	}
	if t.Blocked != nil && t.Blocked.Kind != "" {
		return rej("out-of-phase", "task '%s' is blocked by a %s request (%s): no phase moves until the spec is revised and approved. Resume with: sdkit sdd revalidate --sha <new approved revision>", t.Task, t.Blocked.Kind, t.Blocked.Ref)
	}
	mg := t.Managed && e.Mgd != nil
	if index(target) < 0 {
		return rej("unknown-phase", "unknown phase '%s' (phases: %s)", target, strings.Join(Phases, " "))
	}
	want := Next(cur)
	if target == "verified" && (cur == "verified" || cur == "pr") {
		if fp, _ := e.Fingerprint(); t.VerifiedFingerprint != fp { // re-verification: a later edit made the evidence stale
			want = "verified"
		}
	}
	if target != want {
		if want == "" {
			want = "none"
		}
		return rej("not-next", "cannot go from '%s' to '%s'; the next phase is '%s'. Phases are not skipped.", cur, target, want)
	}
	var mut func(*Task) // written with the move below, never before it
	var att string
	managed := func() *Refusal {
		if !mg {
			return nil
		}
		apply, r := e.Mgd.Gate(e, target, t)
		if apply != nil {
			mut = apply
		}
		return r
	}
	switch target {
	case "proposed":
		proposal := e.abs(t.Proposal)
		text, err := os.ReadFile(proposal)
		if err != nil {
			return rej("no-artifact", "proposal not found: %s", t.Proposal)
		}
		dir := path.Dir(filepath.ToSlash(t.Proposal))
		if e.setting("ROUTING", "off") == "on" && e.setting("ROUTE_POLICY", "tiered") == "tiered" && e.setting("ROUTE_REQUIRE_SIZE", "1") != "0" {
			if tasks, err := os.ReadFile(filepath.Join(filepath.Dir(proposal), "tasks.md")); err == nil {
				if no := missingSizes(string(tasks)); no != "" {
					return rej("no-task-size", "%s/tasks.md: %shave no size:. ROUTING=on routes the model by task size. Read references/model-routing.md, give every delegated task size: small|medium|large, and run this again (ROUTE_REQUIRE_SIZE=0 in sdkit.env gives up per-task routing).", dir, no)
				}
			}
		}
		if !hasDoD(string(text)) {
			return rej("no-dod", "%s has no 'Definition of Done' section with at least one list item. Each acceptance criterion needs a testable line.", t.Proposal)
		}
		if risk == "high" {
			d, err := os.ReadFile(filepath.Join(filepath.Dir(proposal), "design.md"))
			if err != nil || !hasRaceSection(string(d)) {
				return rej("no-design", "high risk: %s/design.md needs a race / ordering / timing scenario section.", dir)
			}
		}
		if r := managed(); r != nil {
			return r
		}
	case "red":
		if testCmd == "" {
			return rej("no-artifact", "--test-cmd is required: the command that runs the new tests")
		}
		if e.runTest(target, testCmd) == 0 {
			return rej("tests-pass", "the tests PASS, so they prove nothing yet. They must fail for the right reason first (see %s/.sdd/last-test.log).", e.kitPath())
		}
		cmd := testCmd
		mut = func(c *Task) { c.TestCmd = cmd }
	case "green":
		red := t.TestCmd
		if testCmd != "" && red != "" && testCmd != red {
			return rej("test-cmd-changed", "GREEN re-runs the command that failed at RED ('%s'); a different command proves nothing about those tests. Run 'sdkit sdd advance green' without --test-cmd.", red)
		}
		if red != "" {
			testCmd = red
		}
		if testCmd == "" {
			return rej("no-artifact", "--test-cmd is required")
		}
		if r := e.pendingGate("green", t, profile); r != nil {
			return r
		}
		if e.runTest(target, testCmd) != 0 {
			e.warnf("%s", tail(filepath.Join(e.stateDir(), "last-test.log"), 20))
			return rej("tests-fail", "the tests still fail (full output: %s/.sdd/last-test.log)", e.kitPath())
		}
		if r := e.enforceFloor(risk); r != nil {
			return r
		}
	case "verified":
		if r := e.enforceFloor(risk); r != nil {
			return r
		}
		if _, err := os.Stat(e.abs(t.Proposal)); err != nil {
			return rej("no-artifact", "proposal not found: %s (it was there at 'proposed')", t.Proposal)
		}
		if r := managed(); r != nil {
			return r
		}
		if r := e.pendingGate("verified", t, profile); r != nil {
			return r
		}
		fp, err := e.Fingerprint()
		if err != nil {
			e.warnf("sdd: %s\n", err)
			return rej("no-fingerprint", "cannot fingerprint the diff (see the message above)")
		}
		if !needsVerifier(risk, profile) {
			if e.runVerify() != 0 {
				e.warnf("%s", tail(filepath.Join(e.stateDir(), "last-verify.log"), 20))
				e.emit("verify.result", "outcome", "fail", "source", "verify-cmd")
				return rej("verify-failed", "the verify command failed")
			}
			e.emit("verify.result", "outcome", "pass", "source", "verify-cmd")
		} else {
			res, why := "", risk+" risk"
			if t.Verdict != nil {
				res = t.Verdict.Result
			}
			if profile == "high-assurance" {
				why = "profile high-assurance"
			}
			if res == "BLOCK" {
				return rej("verdict-block", "the verifier BLOCKed this change. %s", e.retryHint(t))
			}
			if res != "PASS" {
				if res == "" {
					res = "none"
				}
				return rej("no-verdict", "%s needs an independent verifier PASS (current: %s). Run the verifier agent on a clean context; its verdict is recorded automatically.", why, res)
			}
			if t.Verdict.Fingerprint != fp {
				return rej("stale-verdict", "the diff changed after the verifier ran. Run the verifier again on the current diff.")
			}
		}
		if r := e.evidenceGate(t, "live", fp); r != nil {
			return r
		}
		att = fp
		mut = func(c *Task) { c.VerifiedFingerprint = fp }
	case "pr": // high risk: the evidence must still match the diff that goes into the PR
		if r := e.evidenceGate(t, "live", ""); r != nil {
			return r
		}
		if r := managed(); r != nil {
			return r
		}
	case "done": // the merge moved the diff, so the check is against what was verified
		if r := e.evidenceGate(t, "recorded", ""); r != nil {
			return r
		}
		if r := managed(); r != nil {
			return r
		}
	}
	// the move is made only if the phase is still the one this advance checked: a second advance at the same moment loses here
	evGate := target == "verified" || target == "pr" || target == "done"
	err := e.put(func(c *Task) error {
		if c.Phase != cur || c.Blocked != nil || evGate && c.EvSeq != seq {
			return errMoved
		}
		if mut != nil {
			mut(c)
		}
		c.Phase = target
		c.History = append(c.History, Step{Phase: target, At: now()})
		return nil
	})
	if err != nil {
		cp := "?"
		if n := e.load(); n != nil {
			cp = n.Phase
		}
		return rej("concurrent", "the task changed while this advance was running (phase '%s', evidence recorded meanwhile or another advance won); nothing was changed by this call.", cp)
	}
	if att != "" {
		e.writeAttestation(att) // after the move: a refused or lost advance never leaves a proof behind
	}
	e.emit("phase.enter", "from", cur, "to", target)
	if target == "done" {
		e.emit("task.end", "outcome", "completed")
		e.finalize("completed")
		e.close()
		e.printf("task closed\n")
		return nil
	}
	e.printf("phase: %s\n", target)
	if e.Get("CONTEXT_HINT") == "1" { // a phase boundary is the cheapest place to drop the conversation
		switch target {
		case "proposed":
			e.warnf("boundary: the proposal is in %s/ and the plan in %s/work/. If this session is large, /clear (or a new session) and resume with the task id loses nothing.\n", path.Dir(filepath.ToSlash(t.Proposal)), e.kitPath())
		case "green":
			e.warnf("boundary: tests are green. Verification starts from files only, so a /clear or new session here loses nothing.\n")
		}
	}
	return nil
}

var errMoved = errors.New("phase moved")

// pendingGate: agents still in flight block green and verified, but only where the profile asks for it (high-assurance).
func (e *Env) pendingGate(phase string, t *Task, profile string) *Refusal {
	if profile != "high-assurance" || PendingAgents == nil || workflow.Contracts(profile, e.Get("CONTRACTS") == "1", "developer") != "required" {
		return nil
	}
	if pend := strings.Join(PendingAgents(t.Task), " "); pend != "" {
		return rej("agents-pending", "agents still in flight or interrupted: %s. Wait for them, or close an interrupted one (sdkit contract close <id> --reason interrupted) and retry it once (sdkit state %s retry <id>).", pend, t.Task)
	}
	return nil
}

// retryHint: after a BLOCK, the one automatic retry may already be spent; then the next step is the user.
func (e *Env) retryHint(t *Task) string {
	if s, err := state.Read(e.P, t.Task); err == nil && len(s.Retries) > 0 {
		return "The one automatic retry is already spent: stop and ask the user."
	}
	return "Implementation bug: one retry (state.sh <slug> retry <task>), then the verifier again. Spec gap: back to the plan gate and tell the user."
}
