package sdd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/store"
	"github.com/wecii/sdkit/internal/workflow"
)

var reTicket = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

// mStart is `start --ticket KEY`: the task contract is fetched, the pinned revision checked, the DoD seeded.
func (e *Env) mStart(args []string) int {
	key, risk, profile := "", "", "standard"
	for i := 0; i < len(args); i++ {
		val := ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch args[i] {
		case "--ticket":
			key = val
		case "--risk":
			risk = val
		case "--profile":
			profile = val
		default:
			return e.die("managed mode (SDD_MANAGED=on): tasks come from Jira. Usage: start --ticket <KEY> [--risk low|medium|high] [--profile small|standard|high-assurance] (got '" + args[i] + "')")
		}
		i++
	}
	if key == "" {
		return e.die("managed mode (SDD_MANAGED=on): tasks come from Jira. Start one with: sdkit sdd start --ticket <KEY>")
	}
	if !reTicket.MatchString(key) {
		return e.die("--ticket '" + key + "' is not a Jira issue key (PROJ-123)")
	}
	if risk != "" && risk != "low" && risk != "medium" && risk != "high" {
		return e.die("--risk must be low, medium or high (it can only raise the task's risk)")
	}
	if err := workflow.Check(profile, "low"); err != nil { // the profile name only; small vs risk is judged below
		return e.die(err.Error())
	}
	if o := e.load(); o != nil {
		return e.die(fmt.Sprintf("task '%s' is still open (phase %s). Finish it or run: sdkit sdd abort", o.Task, o.Phase))
	}
	if e.Mode() != "off" {
		if why := FloorValidate(e.Get("RISK_FLOOR_GLOBS")); why != "" {
			return e.die("RISK_FLOOR_GLOBS is invalid: " + why + " (expected level:glob,level:glob with level low|medium|high)")
		}
	}
	c, why := e.mContract(key)
	if why != "" {
		return e.die(why)
	}
	tr := tstr(c, "risk")
	if risk != "" && rank(risk) < rank(tr) {
		return e.die("the task contract of " + key + " says risk " + tr + ": a label can raise it, never lower it")
	}
	if risk == "" {
		risk = tr
	}
	if err := workflow.Check(profile, risk); err != nil {
		return e.die(err.Error())
	}
	t := &Task{Task: key, Risk: risk, Profile: profile, Proposal: e.kitPath() + "/work/" + key + "/proposal.md", Phase: "intake", Managed: true, Ticket: key,
		SpecID: tstr(c, "specId"), SpecVersion: tstr(c, "specVersion"), SpecSha: tstr(c, "specSha"), AcceptanceCriteria: tlist(c, "acceptanceCriteria")}
	if why := e.mSha(t.SpecSha); why != "" {
		return e.die(why)
	}
	if why := e.mStale(t); why != "" {
		return e.die(why)
	}
	if why := e.mDeps(key); why != "" {
		return e.die(why)
	}
	kept := false
	if _, err := os.Stat(e.abs(t.Proposal)); err == nil {
		kept = true
	} else if err := writeDoD(e.abs(t.Proposal), c, e.specScenarios(t.SpecID, t.SpecSha)); err != nil {
		return e.die("cannot write " + t.Proposal)
	}
	at := now()
	t.StartedAt, t.History = at, []Step{{Phase: "intake", At: at}}
	xset(t, "externalId", c["externalId"])
	xset(t, "taskRisk", c["risk"])
	xset(t, "deps", c["dependencies"])
	xset(t, "scope", c["scope"])
	xset(t, "parent", c["parent"])
	err := store.Update(e.statePath(), func(old []byte) ([]byte, error) {
		if old != nil {
			return nil, errExists
		}
		return t.Marshal(), nil
	})
	if err != nil {
		if o := e.load(); o != nil {
			return e.die(fmt.Sprintf("task '%s' is still open (phase %s). Finish it or run: sdkit sdd abort", o.Task, o.Phase))
		}
		return e.die(err.Error())
	}
	e.emit("task.start", "task", key, "risk", risk, "profile", profile, "phase", "intake")
	if kept {
		e.warnf("sdd: kept the existing %s (delete it before start to regenerate it from the contract)\n", t.Proposal)
	}
	e.printf("task '%s' opened (managed: spec %s v%s at %s, risk=%s, profile=%s, phase=intake). The DoD is seeded from the task's acceptance criteria in %s; refine it from the spec, then: sdkit sdd advance proposed\n",
		key, t.SpecID, t.SpecVersion, short(t.SpecSha), risk, profile, t.Proposal)
	return 0
}

// mBlock is `block --spec-change --reason R [--section S] [--proposal TEXT]`: the change request is filed first; the flag is set only when that succeeded.
func (e *Env) mBlock(args []string) int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	kind, reason, section, prop := "", "", "", ""
	for i := 0; i < len(args); i++ {
		val := ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch args[i] {
		case "--spec-change":
			kind = "spec-change"
			continue
		case "--reason":
			reason = val
		case "--section":
			section = val
		case "--proposal":
			prop = val
		default:
			return e.die("unknown option " + args[i])
		}
		i++
	}
	if kind != "spec-change" || strings.TrimSpace(reason) == "" {
		return e.die("usage: block --spec-change --reason <what in the spec is wrong> [--section <spec section>] [--proposal <suggested fix>]")
	}
	if !t.Managed {
		return e.die("block --spec-change is for managed tasks (opened with start --ticket); this one has no ticket")
	}
	if blockedBy(t) {
		return e.die(fmt.Sprintf("already blocked by %s (%s, %s). Resume with: sdkit sdd revalidate --sha <new approved revision>", t.Blocked.Ref, t.Blocked.Kind, t.Blocked.At))
	}
	body := map[string]string{"reason": reason, "specId": t.SpecID, "specSha": t.SpecSha}
	if section != "" {
		body["section"] = section
	}
	if prop != "" {
		body["proposal"] = prop
	}
	j := e.jiraCfg()
	ref, err := j.ChangeRequest(t.Ticket, jiraJSON(body))
	if err != nil || ref == "" {
		rc := 5
		if err != nil {
			rc = e.jiraErr(err)
		}
		return e.die(fmt.Sprintf("the change request for %s could not be filed (jira.sh change-request exit %d): the task is NOT blocked and nothing was recorded. Fix the Jira access and run this again; do not keep implementing against the defect.", t.Ticket, rc))
	}
	var sec *string
	if section != "" {
		sec = &section
	}
	if err := e.put(func(c *Task) error {
		c.Blocked = &Blocked{Kind: "spec-change", Ref: ref, Reason: reason, Section: sec, At: now()}
		c.EvSeq++
		return nil
	}); err != nil {
		return e.die(fmt.Sprintf("the change request %s was filed, but the state could not be written: run 'sdkit sdd block' again only after checking %s (it would file a second one)", ref, ref))
	}
	st := e.setting("JIRA_BLOCKED_STATUS", "Blocked")
	moved := true
	if err := j.Transition(t.Ticket, st); err != nil {
		moved = false
		e.jiraErr(err)
		e.warnf("sdd: warning: %s could not be moved to '%s' (JIRA_BLOCKED_STATUS); the change request and the block stand\n", t.Ticket, st)
	} else {
		e.printf("%s moved to '%s'\n", t.Ticket, st)
	}
	_ = e.put(func(c *Task) error { c.Blocked.Transitioned = &moved; return nil })
	e.printf("blocked: spec change request %s filed against %s. advance and gate refuse until: sdkit sdd revalidate --sha <new approved revision>\n", ref, t.Ticket)
	return 0
}

func jiraJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// mRevalidate is `revalidate --sha NEW`: only while blocked; NEW must differ from the pinned SHA, be approved and be what
// the task contract now pins. The block is cleared, the old revision's evidence dropped and the proposed gate run again.
func (e *Env) mRevalidate(args []string) int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	nw := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--sha" {
			return e.die("unknown option " + args[i])
		}
		if i+1 < len(args) {
			nw = args[i+1]
		} else {
			nw = ""
		}
		i++
	}
	if nw == "" {
		return e.die("usage: revalidate --sha <new approved spec revision>")
	}
	if !t.Managed {
		return e.die("revalidate is for managed tasks (opened with start --ticket)")
	}
	if !blockedBy(t) {
		return e.die(fmt.Sprintf("revalidate only resumes a blocked task (sdkit sdd block --spec-change); '%s' is not blocked", t.Task))
	}
	old, sp, d := t.SpecSha, e.specsDir(), e.specsPath()
	full := gitx.RevParse(d, nw)
	if full == "" {
		return e.die(fmt.Sprintf("%s is not a commit in %s/ (git -C %s fetch)", nw, sp, sp))
	}
	if full == old {
		return e.die(fmt.Sprintf("%s is the revision the task is already pinned to: a blocked task resumes only on a changed, approved spec revision", nw))
	}
	if SpecApprovedSHA == nil {
		return e.die("spec approved-sha is not available in this build: cannot check that " + short(full) + " is approved")
	}
	if err := SpecApprovedSHA(d, t.SpecID, full, e.Err); err != nil {
		return e.die(fmt.Sprintf("spec %s is not approved at %s (spec.sh approved-sha in %s/): the change request must be approved and merged first", t.SpecID, short(full), sp))
	}
	c, why := e.mContract(t.Ticket)
	if why != "" {
		return e.die(why)
	}
	if id := tstr(c, "specId"); id != t.SpecID {
		return e.die(fmt.Sprintf("the task contract of %s now names spec '%s', not '%s': abort and start the task again", t.Ticket, id, t.SpecID))
	}
	if pin := tstr(c, "specSha"); pin != full {
		return e.die(fmt.Sprintf("the task contract of %s still pins %s: update the subtask to %s first (generate-tasks / jira.sh upsert), then run this again", t.Ticket, short(pin), short(full)))
	}
	if why := e.mSha(full); why != "" {
		return e.die(why)
	}
	if why := e.mDeps(t.Ticket); why != "" {
		return e.die(why)
	}
	r := t.Risk
	if tr := tstr(c, "risk"); rank(tr) > rank(r) {
		r = tr // the revised task can raise the risk, never lower it
	}
	if t.Profile == "small" && r != "low" {
		return e.die(fmt.Sprintf("the revised task is risk %s, which profile small does not allow: sdkit sdd abort and start it again with --profile standard", r))
	}
	prop := e.abs(t.Proposal)
	if b, err := os.ReadFile(prop); err == nil {
		_ = os.WriteFile(prop+".prev", b, 0o644)
	}
	if err := writeDoD(prop, c, e.specScenarios(tstr(c, "specId"), tstr(c, "specSha"))); err != nil {
		return e.die("cannot write " + t.Proposal)
	}
	cur := t.Phase
	back := index(cur) >= index("proposed")
	ver := tstr(c, "specVersion")
	err := e.put(func(x *Task) error {
		if x.Blocked == nil || x.SpecSha != old {
			return errMoved
		}
		var rv []map[string]string
		_ = json.Unmarshal(x.Extra["revalidations"], &rv)
		xset(x, "revalidations", append(rv, map[string]string{"from": old, "to": full, "ref": x.Blocked.Ref, "phase": x.Phase, "at": now()}))
		x.SpecSha, x.SpecVersion, x.AcceptanceCriteria, x.Risk = full, ver, tlist(c, "acceptanceCriteria"), r
		xset(x, "deps", c["dependencies"])
		xset(x, "scope", c["scope"])
		xset(x, "taskRisk", c["risk"])
		x.Blocked, x.Verdict, x.Evidence, x.VerifiedFingerprint, x.Spawns, x.TestCmd = nil, nil, nil, "", nil, ""
		x.PRHead, x.PRBranch, x.PRBase = "", "", ""
		delete(x.Extra, "synced")
		delete(x.Extra, "syncComment")
		x.EvSeq++
		if back {
			x.Phase = "intake"
			x.History = append(x.History, Step{Phase: "intake", At: now(), Reason: "revalidate"})
		}
		return nil
	})
	if err != nil {
		return e.die("the task changed while revalidating; nothing was changed by this call")
	}
	_ = os.Remove(filepath.Join(e.P.Dir(), "attestations", t.Task+".json")) // the old revision's proof must not satisfy ci-check for the new one
	e.printf("revalidated: '%s' is pinned to %s v%s at %s; the block and the evidence of %s are gone (the old DoD is in %s.prev)\n", t.Task, t.SpecID, ver, short(full), short(old), t.Proposal)
	if cur != "intake" {
		if e.Advance("proposed", "") != 0 {
			e.warnf("sdd: the proposal gate did not hold yet: fix what it names, then sdkit sdd advance proposed\n")
		}
	}
	return 0
}

// mEvidenceSync is `evidence-sync [--pr URL] [--status S|none]`: the evidence summary and the PR link as a comment on the
// ticket, then the status. Idempotent: the same evidence, PR and status are sent once.
func (e *Env) mEvidenceSync(args []string) int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	pr, st := "", e.setting("JIRA_REVIEW_STATUS", "In Review")
	for i := 0; i < len(args); i++ {
		val := ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch args[i] {
		case "--pr":
			pr = val
		case "--status":
			st = val
		default:
			return e.die("unknown option " + args[i])
		}
		i++
	}
	if !t.Managed {
		return e.die("evidence-sync is for managed tasks (opened with start --ticket)")
	}
	if blockedBy(t) {
		return e.die(fmt.Sprintf("the task is blocked by %s: there is no evidence to report until it is revalidated", t.Blocked.Ref))
	}
	if t.Phase != "verified" && t.Phase != "pr" {
		return e.die(fmt.Sprintf("evidence-sync reports verified evidence: the task is in '%s'", t.Phase))
	}
	fp := t.VerifiedFingerprint
	if cur, _ := e.Fingerprint(); fp == "" || fp != cur {
		return e.die("the diff changed after verification: re-verify first (sdkit sdd advance verified), then sync")
	}
	if pr == "" {
		pr = xstr(t, "prUrl")
	}
	if st == "none" {
		st = ""
	}
	key := fp + "|" + pr + "|" + st
	var synced struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(t.Extra["synced"], &synced)
	suffix := ""
	if pr != "" {
		suffix = " and PR"
	}
	if synced.Key == key {
		e.printf("evidence-sync: %s already has this evidence%s; nothing sent\n", t.Ticket, suffix)
		return 0
	}
	var rows strings.Builder
	for _, r := range EvidenceTable(t, fp, evRoles) {
		fmt.Fprintf(&rows, "%s: %s (verdict %s, trust %s)\n", r.Role, r.State, r.Verdict, r.Trust)
	}
	verdict := ""
	if t.Verdict != nil {
		verdict = t.Verdict.Result
	}
	prText := pr
	if prText == "" {
		prText = "not open yet"
	}
	text := fmt.Sprintf("sdkit evidence for %s (task %s)\nspec: %s v%s at %s\nrisk: %s, profile: %s, phase: %s, verifier verdict: %s\n%s\ndiff fingerprint: %s\nPR: %s",
		t.Ticket, t.Task, t.SpecID, t.SpecVersion, t.SpecSha, t.Risk, t.Profile, t.Phase, verdict, strings.TrimSuffix(rows.String(), "\n"), fp, prText)
	j := e.jiraCfg()
	if xstr(t, "syncComment") != key {
		if err := j.Comment(t.Ticket, text); err != nil {
			e.jiraErr(err)
			return e.die(fmt.Sprintf("the evidence comment on %s failed: nothing was recorded, run evidence-sync again", t.Ticket))
		}
		if err := e.put(func(c *Task) error { xset(c, "syncComment", key); xset(c, "prUrl", pr); return nil }); err != nil {
			return e.die(fmt.Sprintf("the comment is on %s but the state could not be written", t.Ticket))
		}
	}
	if st != "" {
		if err := j.Transition(t.Ticket, st); err != nil {
			e.jiraErr(err)
			return e.die(fmt.Sprintf("the evidence comment is on %s, but its status could not be set to '%s': run evidence-sync again (the comment is not repeated)", t.Ticket, st))
		}
	}
	if err := e.put(func(c *Task) error {
		xset(c, "synced", map[string]string{"key": key, "fingerprint": fp, "pr": pr, "status": st, "at": now()})
		return nil
	}); err != nil {
		return e.die("could not record the sync")
	}
	stText := ""
	if st != "" {
		stText = ", status '" + st + "'"
	}
	pl := ""
	if pr != "" {
		pl = " and the PR link"
	}
	e.printf("evidence-sync: %s has the evidence%s%s\n", t.Ticket, pl, stText)
	return 0
}
