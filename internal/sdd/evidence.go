package sdd

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wecii/sdkit/internal/workflow"
)

// Three roles judge a change independently: verifier, reviewer, security-auditor. Their verdicts are stored per role in
// state.json (.evidence), bound to the task and to the fingerprint of the diff, with a trust level:
//
//	spawned       produced by an agent run that was REGISTERED when it started (Spawn) and whose registration this record
//	              consumed, on the same diff. Only this satisfies the high-risk gate.
//	unregistered  the host said an agent stopped but no spawn was registered
//	manual        recorded by hand
//
// A claim of a trusted source that cannot be backed by a registration is refused, not downgraded. The latest record per
// role wins, so a newer BLOCK always replaces an older PASS; a manual PASS never displaces a trusted PASS of the same diff.
var evRoles = []string{"verifier", "reviewer", "security-auditor"}

// Role normalises an agent type to an evidence role ("kit:verifier-deep" is "verifier"); ok is false for any other.
func Role(s string) (string, bool) {
	r := s[strings.LastIndex(s, ":")+1:]
	r = strings.TrimSuffix(r, "-deep")
	for _, x := range evRoles {
		if x == r {
			return r, true
		}
	}
	return "", false
}

// Spawn registers that an independent role is being started, remembering the diff it will see. Hooks and run-role only.
func (e *Env) Spawn(role string, args []string) int {
	role, ok := Role(role)
	src, run := "hook", ""
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--source":
			src = args[i+1]
		case "--run":
			run = args[i+1]
		}
	}
	if !ok || e.load() == nil {
		return 0
	}
	fp, err := e.Fingerprint()
	if err != nil {
		return 1
	}
	e.put(func(t *Task) error {
		t.Spawns = append(t.Spawns, Spawn{role, src, run, fp, now()})
		if n := len(t.Spawns); n > 16 {
			t.Spawns = t.Spawns[n-16:]
		}
		return nil
	})
	return 0
}

// SpawnClose drops the registration of an agent that never answered (host error, killed); nothing is recorded as evidence.
//
//	--run ID [--reason WHY]           run-role: the registration with that run id
//	--role ROLE --source hook         Claude Code hook: the newest run-less registration of the role
func (e *Env) SpawnClose(args []string) int {
	run, why, role, src := "", "interrupted", "", "hook"
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--run":
			run = args[i+1]
		case "--reason":
			why = args[i+1]
		case "--role":
			r, ok := Role(args[i+1])
			if !ok {
				return 0
			}
			role = r
		case "--source":
			src = args[i+1]
		}
	}
	if e.load() == nil || run == "" && role == "" {
		return 0
	}
	e.put(func(t *Task) error {
		i := -1
		for j, s := range t.Spawns {
			if run != "" && s.Run == run || run == "" && s.Role == role && s.Source == src && s.Run == "" {
				i = j
				if run != "" {
					break
				}
			}
		}
		var s Spawn
		if i >= 0 {
			s = t.Spawns[i]
			t.Spawns = append(t.Spawns[:i:i], t.Spawns[i+1:]...)
		}
		if i < 0 && run == "" {
			return nil
		}
		r := s.Role
		if r == "" {
			r = role
		}
		t.logEvidence(LogEntry{Role: r, Run: run, At: now(), Reason: why})
		return nil
	})
	return 0
}

func (t *Task) logEvidence(l LogEntry) {
	t.EvidenceLog = append(t.EvidenceLog, l)
	if n := len(t.EvidenceLog); n > 40 {
		t.EvidenceLog = t.EvidenceLog[n-40:]
	}
}

var evCount atomic.Int64

// Evidence records one role's verdict against the diff as it is right now. src is manual (default), hook or run-role.
// code: 0 recorded (msg says which trust), 1 refused (msg says why), 4 no task is open (nothing to record against).
func (e *Env) Evidence(role, result, src, run, task string) (msg string, code int) {
	if e.load() == nil {
		return "no active task", 4
	}
	role, ok := Role(role)
	if !ok {
		return "evidence role must be verifier, reviewer or security-auditor", 1
	}
	if result != "PASS" && result != "BLOCK" {
		return "verdict must be PASS or BLOCK", 1
	}
	if src == "" {
		src = "manual"
	}
	if src != "manual" && src != "hook" && src != "run-role" {
		return "--source must be manual, hook or run-role", 1
	}
	fp, err := e.Fingerprint()
	if err != nil {
		return "cannot fingerprint the diff, nothing recorded", 1
	}
	var last LogEntry
	err = e.put(func(t *Task) error {
		last = t.record(role, result, src, run, task, fp)
		return nil
	})
	if err == errNoTask {
		return "no active task", 4
	}
	if err != nil {
		return "could not write the state", 1
	}
	if last.Accepted {
		if role == "verifier" {
			e.emit("verify.result", "outcome", result, "source", "verifier")
			if result == "BLOCK" {
				e.emit("failure", "category", "verdict-block", "source", "verifier")
			}
		}
		return fmt.Sprintf("evidence: %s %s trust=%s", role, result, last.Trust), 0
	}
	if last.Reason == "kept-trusted" {
		return fmt.Sprintf("evidence: %s %s ignored (a trusted PASS for this diff is already recorded)", role, result), 0
	}
	return fmt.Sprintf("evidence refused (%s): %s %s", last.Reason, role, result), 1
}

// record applies the trust rules and returns the log entry of this attempt.
func (t *Task) record(role, result, src, run, task, fp string) LogEntry {
	n := fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), evCount.Add(1), time.Now().Unix())
	l := LogEntry{N: n, Role: role, Result: result, Run: run, Source: src, FP: fp, At: now()}
	refuse := func(reason string, gone ...Spawn) LogEntry {
		t.drop(gone)
		l.Trust, l.Reason = "-", reason
		t.logEvidence(l)
		return l
	}
	accept := func(trust string, gone ...Spawn) LogEntry {
		t.drop(gone)
		t.EvSeq++
		if run != "" {
			t.UsedRuns = append(t.UsedRuns, UsedRun{run, role})
			if n := len(t.UsedRuns); n > 200 {
				t.UsedRuns = t.UsedRuns[n-200:]
			}
		}
		l.Trust = trust
		if old, ok := t.Evidence[role]; ok && trust != "spawned" && result == "PASS" && old.Trust == "spawned" && old.Result == "PASS" && old.Fingerprint == fp {
			l.Reason = "kept-trusted"
			t.logEvidence(l)
			return l
		}
		if t.Evidence == nil {
			t.Evidence = map[string]Record{}
		}
		t.Evidence[role] = Record{result, fp, t.Task, trust, src, run, l.At, t.EvSeq}
		if role == "verifier" {
			t.Verdict = &Verdict{result, fp, l.At}
		}
		l.Accepted, l.Reason = true, "recorded"
		t.logEvidence(l)
		return l
	}
	var mine []Spawn
	for _, s := range t.Spawns {
		if s.Role == role && s.Source == src {
			mine = append(mine, s)
		}
	}
	var same, other bool
	for _, u := range t.UsedRuns {
		if run != "" && u.Run == run {
			same, other = same || u.Role == role, other || u.Role != role
		}
	}
	switch {
	case task != "" && task != t.Task:
		return refuse("task-mismatch")
	case other:
		return refuse("role-conflict")
	case same:
		return refuse("duplicate")
	case src == "run-role":
		var reg *Spawn
		for i := range mine {
			if mine[i].Run == run {
				reg = &mine[i]
			}
		}
		if run == "" || reg == nil {
			return refuse("unknown-run")
		}
		if reg.FP != fp {
			return refuse("stale-at-record", *reg)
		}
		return accept("spawned", *reg)
	case src == "hook" && run != "":
		var reg *Spawn
		var old []Spawn
		for i := range mine {
			if mine[i].FP == fp {
				reg = &mine[i]
			} else {
				old = append(old, mine[i])
			}
		}
		switch {
		case len(old) > 0:
			return refuse("stale-at-record", old...)
		case reg != nil:
			return accept("spawned", *reg)
		}
		return accept("unregistered")
	}
	return accept("manual")
}

// drop removes the registrations in gone (by value).
func (t *Task) drop(gone []Spawn) {
	var keep []Spawn
	for _, s := range t.Spawns {
		g := false
		for _, x := range gone {
			g = g || x == s
		}
		if !g {
			keep = append(keep, s)
		}
	}
	t.Spawns = keep
}

// Recorder is the sdd-backed verdict.Recorder (sdkit verdict parse --record): the verdict goes to Evidence, and only a
// refusal is reported; no open task is silent.
type Recorder struct{ Env func() *Env }

// Record implements verdict.Recorder.
func (r Recorder) Record(role, verdict, source, run, task string) (string, bool) {
	msg, code := r.Env().Evidence(role, verdict, source, run, task)
	return msg, code == 1
}

// Row is one role's validity against a diff.
type Row struct {
	Role     string `json:"role"`
	State    string `json:"state"`
	Verdict  string `json:"verdict"`
	Trust    string `json:"trust"`
	Run      string `json:"run"`
	Required bool   `json:"required"`
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// EvidenceTable gives one row per role. The state is the whole story of its validity: valid | missing | block | wrong-task |
// untrusted | stale | conflict (the same run id under two roles). ref is the fingerprint judged against: the live diff, or
// the verified one after the merge (the diff no longer exists, so `done` cannot recompute it).
func EvidenceTable(t *Task, ref string, roles []string) []Row {
	var rows []Row
	for _, r := range roles {
		x, ok := t.Evidence[r]
		st := "valid"
		runs := 0
		for _, o := range t.Evidence {
			if o.Run == x.Run {
				runs++
			}
		}
		switch {
		case !ok:
			st = "missing"
		case x.Result != "PASS":
			st = "block"
		case x.Task != t.Task:
			st = "wrong-task"
		case x.Trust != "spawned":
			st = "untrusted"
		case ref == "" || x.Fingerprint != ref:
			st = "stale"
		case x.Run == "" || runs > 1:
			st = "conflict"
		}
		row := Row{Role: r, State: st, Verdict: "-", Trust: "-", Run: "-"}
		if ok {
			row.Verdict, row.Trust, row.Run = x.Result, x.Trust, dash(x.Run)
		}
		rows = append(rows, row)
	}
	return rows
}

// required: the roles the open task's risk requires independent evidence from.
func required(t *Task) []string { return workflow.EvidenceRoles(t.Risk) }

// evidenceGate refuses (and says why) unless every required role has valid evidence. kind is "live" (against fp, or the
// diff as it is now when fp is "") or "recorded" (against the fingerprint verified).
func (e *Env) evidenceGate(t *Task, kind, fp string) *Refusal {
	roles := required(t)
	if len(roles) == 0 {
		return nil
	}
	if kind == "live" && fp == "" {
		fp, _ = e.Fingerprint()
	} else if kind == "recorded" {
		fp = t.VerifiedFingerprint
	}
	rows := EvidenceTable(t, fp, roles)
	if len(rows) != len(roles) {
		return rej("no-evidence", "the evidence state could not be read (expected %d roles, got %d): treated as missing.", len(roles), len(rows))
	}
	for _, r := range rows {
		role := r.Role
		switch r.State {
		case "valid":
		case "missing":
			return rej("no-evidence", "risk high needs an independent %s verdict (current: none). Run the %s agent on a clean context; its verdict is recorded automatically (Claude Code hook, or scripts/run-role.sh).", role, role)
		case "block":
			return rej("evidence-block", "the %s BLOCKed this change. %s", role, e.retryHint(t))
		case "wrong-task":
			return rej("task-mismatch", "the recorded %s evidence belongs to another task.", role)
		case "untrusted":
			return rej("untrusted-evidence", "the %s verdict was not produced by a registered agent run (recorded by hand, or the spawn was never registered), so it cannot satisfy risk high. Run the %s with the host's agent tool on Claude Code, or with scripts/run-role.sh.", role, role)
		case "stale":
			return rej("stale-evidence", "the diff changed after the %s ran (or no diff was verified). Run the %s again on the current diff.", role, role)
		case "conflict":
			return rej("role-conflict", "the %s evidence shares an agent run with another role: each role must be a separate agent run.", role)
		default:
			return rej("no-evidence", "%s evidence is %s.", role, r.State)
		}
	}
	return nil
}

// ShowEvidence is the read-only view: every role and its validity against the diff as it is now.
func (e *Env) ShowEvidence(args []string) int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	js, withFP := len(args) > 0 && args[0] == "--json", len(args) > 1 && args[1] == "--with-fingerprint"
	fp, _ := e.Fingerprint()
	rows := EvidenceTable(t, fp, evRoles)
	req := map[string]bool{}
	for _, r := range required(t) {
		req[r] = true
	}
	for i := range rows {
		rows[i].Required = req[rows[i].Role]
	}
	switch {
	case js && withFP:
		b, _ := json.MarshalIndent(struct {
			Fingerprint string `json:"fingerprint"`
			Roles       []Row  `json:"roles"`
		}{fp, rows}, "", "  ")
		e.printf("%s\n", b)
	case js:
		b, _ := json.MarshalIndent(rows, "", "  ")
		e.printf("%s\n", b)
	default:
		for _, r := range rows {
			mark := ""
			if r.Required {
				mark = "  (required)"
			}
			e.printf("%-18s %-10s verdict=%-5s trust=%-12s run=%s%s\n", r.Role, r.State, r.Verdict, r.Trust, r.Run, mark)
		}
	}
	return 0
}
