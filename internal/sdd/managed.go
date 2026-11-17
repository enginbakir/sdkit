package sdd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/jira"
	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/store"
)

// Managed execution (SDD_MANAGED=on): the task is a Jira subtask whose task contract pins an approved spec revision
// (specSha) in the read-only SPECS_DIR submodule. No second state machine: the phases and gates of phase.go stay, and this
// file only ADDS checks plus the orthogonal .blocked flag. A Jira or git call that fails refuses; it never records success.

// SpecApprovedSHA is `spec approved-sha <id> --sha S` run in the specs checkout dir; it writes its reasons to w and
// returns an error when the revision is not approved. P3-09 supplies it; nil means it is not available.
var SpecApprovedSHA func(dir, id, sha string, w io.Writer) error

type managed struct{}

// The setting is checked inside (Env.managed), as the bash did: Mgd is always set.
func init() { Mgd = managed{} }

func (managed) Command(e *Env, cmd string, args []string) int {
	switch cmd {
	case "start":
		return e.mStart(args)
	case "block":
		return e.mBlock(args)
	case "revalidate":
		return e.mRevalidate(args)
	case "evidence-sync":
		return e.mEvidenceSync(args)
	case "done":
		if len(args) == 0 || args[0] != "--merged" {
			return e.die("usage: done --merged (the merge is checked, never declared: the PR head must be in the resolved base)")
		}
		return e.Advance("done", "")
	}
	return e.die("unknown managed command " + cmd)
}

// Rule has no Env, so SPECS_DIR is read from the process environment only (default specs).
func (managed) Rule(phase string) string {
	sp := strings.TrimRight(os.Getenv("SPECS_DIR"), "/")
	if sp == "" {
		sp = "specs"
	}
	switch phase {
	case "proposed":
		return "the Jira task contract is complete and still pins the task's specSha, the specSha resolves in " + sp + "/, dependencies are done, dod-check.sh passes"
	case "verified":
		return "the pinned specSha still resolves in " + sp + "/"
	case "done":
		return "the PR head recorded at 'pr' is merged into the resolved base (never self-declared)"
	}
	return ""
}

func (managed) Gate(e *Env, phase string, t *Task) (func(*Task), *Refusal) {
	switch phase {
	case "proposed":
		return nil, e.mProposedGate(t)
	case "verified":
		if why := e.mSha(t.SpecSha); why != "" {
			return nil, rej("missing-artifact", "%s", why)
		}
		if why := e.mStale(t); why != "" {
			return nil, rej("inconsistent", "%s", why)
		}
	case "pr": // remember what goes into the PR, so `done` can check that exactly this was merged
		head, err := gitx.Run(e.Root, "rev-parse", "-q", "--verify", "HEAD")
		if err != nil || head == "" {
			return nil, rej("no-artifact", "cannot read HEAD")
		}
		br, _ := gitx.Run(e.Root, "rev-parse", "--abbrev-ref", "HEAD")
		bs := e.Base()
		return func(c *Task) { c.PRHead, c.PRBranch, c.PRBase = head, br, bs }, nil
	case "done":
		if why := e.mMerged(t); why != "" {
			return nil, rej("out-of-phase", "%s", why)
		}
	}
	return nil, nil
}

func (managed) Push(e *Env, t *Task) string { return e.mStale(t) }

func (managed) Edit(e *Env, rel string) string {
	sp := e.specsDir()
	if rel == sp || strings.HasPrefix(rel, sp+"/") {
		return "Managed mode: " + sp + "/ is the read-only approved-specs submodule and is never edited from this repo. A spec defect is a change request: sdkit sdd block --spec-change --reason <why> [--section <section>]."
	}
	return ""
}

// specsDir is SPECS_DIR (the approved-specs submodule), slash form, no trailing slash.
func (e *Env) specsDir() string { return strings.TrimRight(e.setting("SPECS_DIR", "specs"), "/") }

func (e *Env) jiraCfg() *jira.Config { return jira.Load(e.Root, os.Getenv) }

// jiraErr prints a failed jira call as the CLI does and returns its exit code.
func (e *Env) jiraErr(err error) int {
	fmt.Fprintln(e.Err, "jira: "+err.Error())
	var je *jira.Err
	if errors.As(err, &je) {
		return je.Code
	}
	return 5
}

// Task keys the managed mode keeps beyond Task's own fields live in Extra.
func xstr(t *Task, k string) string {
	var s string
	if json.Unmarshal(t.Extra[k], &s) != nil {
		return ""
	}
	return s
}

func xset(t *Task, k string, v any) {
	if t.Extra == nil {
		t.Extra = map[string]json.RawMessage{}
	}
	b, _ := json.Marshal(v)
	t.Extra[k] = b
}

// tstr is a contract field as text (a number is written as jq would).
func tstr(t jira.Task, k string) string {
	switch v := t[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(t[k])
}

func tlist(t jira.Task, k string) []string {
	a, _ := t[k].([]any)
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// mContract fetches and validates the task contract of key; why is the refusal ("" when fine).
func (e *Env) mContract(key string) (jira.Task, string) {
	t, err := e.jiraCfg().Fetch(key)
	if err != nil || len(t) == 0 {
		rc := 5
		if err != nil {
			rc = e.jiraErr(err)
		}
		return nil, fmt.Sprintf("the task contract of %s could not be read (jira.sh fetch exit %d: 3 egress/auth, 4 contract incomplete, 5 not found)", key, rc)
	}
	if err := jira.Check(t); err != nil {
		fmt.Fprintln(e.Err, "jira: "+err.Error())
		return nil, fmt.Sprintf("the task contract of %s is incomplete or malformed (jira.sh validate-task)", key)
	}
	if g, _ := t["group"].(bool); g {
		return nil, fmt.Sprintf("%s is a group task: it is done when its role subtasks are, and only they are implemented. Start one of its subtasks (the one for your role).", key)
	}
	return t, ""
}

// mDeps is "" only when every dependency of key is done.
func (e *Env) mDeps(key string) string {
	p, err := e.jiraCfg().DepsReady(key)
	if err != nil {
		return fmt.Sprintf("the dependencies of %s could not be checked (jira.sh deps-ready exit %d)", key, e.jiraErr(err))
	}
	if len(p) == 0 {
		return ""
	}
	var l []string
	for _, d := range p {
		l = append(l, fmt.Sprintf("%v (%v)", d["key"], d["status"]))
	}
	return fmt.Sprintf("the dependencies of %s are not done: %s. Start it when they are.", key, strings.Join(l, ", "))
}

func (e *Env) specsPath() string { return filepath.Join(e.Root, filepath.FromSlash(e.specsDir())) }

// mSha: the pinned revision is a commit in the SPECS_DIR submodule AND its checked-out HEAD contains it.
func (e *Env) mSha(sha string) string {
	sp, d := e.specsDir(), e.specsPath()
	if sha == "" || strings.Trim(sha, "0123456789abcdef") != "" {
		return fmt.Sprintf("specSha '%s' is not a commit id", sha)
	}
	if len(sha) != 40 {
		return fmt.Sprintf("specSha '%s' is not a full 40-character commit id", sha)
	}
	if _, err := os.Stat(filepath.Join(d, ".git")); err != nil {
		return fmt.Sprintf("%s/ is not a checked-out git submodule (git submodule update --init %s)", sp, sp)
	}
	if _, err := gitx.Run(d, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return fmt.Sprintf("the pinned specSha %s is not a commit in %s/ (an unknown or unpublished revision is refused; git -C %s fetch if it is new)", sha, sp, sp)
	}
	head, err := gitx.Run(d, "rev-parse", "-q", "--verify", "HEAD")
	if err != nil {
		return sp + "/ has no checked-out HEAD"
	}
	if _, err := gitx.Run(d, "merge-base", "--is-ancestor", sha, head); err != nil {
		return fmt.Sprintf("%s/ is checked out at %s, which does not contain the pinned specSha %s: update the submodule (git -C %s checkout %s, then commit the pointer)", sp, short(head), short(sha), sp, sha)
	}
	return ""
}

// mStale is gate (e), "the code is going to be merged, so the spec must be too": the specs checkout holds a LATER approved
// revision of the same spec whose applied changes touch an AC of the task. "" otherwise, and on any git problem: this gate
// only blocks on a deliberate finding, never on its own error.
func (e *Env) mStale(t *Task) string {
	if !t.Managed || blockedBy(t) {
		return ""
	}
	sha, id, d, sp := t.SpecSha, t.SpecID, e.specsPath(), e.setting("TASKS_SPEC_PATH", "specs")+"/"+t.SpecID
	head, err := gitx.Run(d, "rev-parse", "-q", "--verify", "HEAD")
	if err != nil || head == sha {
		return ""
	}
	if _, err := gitx.Run(d, "merge-base", "--is-ancestor", sha, head); err != nil {
		return ""
	}
	meta := func(rev string) map[string]any {
		s, err := gitx.Run(d, "show", rev+":"+sp+"/spec.meta.json")
		var m map[string]any
		if err != nil || json.Unmarshal([]byte(s), &m) != nil {
			return nil
		}
		return m
	}
	hm := meta(head)
	if hm == nil || hm["status"] != "approved" {
		return ""
	}
	ver := func(m map[string]any) string {
		switch v := m["version"].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	}
	hv, pv := ver(hm), ""
	if pm := meta(sha); pm != nil {
		pv = ver(pm)
	}
	if hv == "" || hv == pv {
		return ""
	}
	ls, _ := gitx.Run(d, "ls-tree", "-r", "--name-only", head, "--", sp+"/changes")
	hit := ""
	for _, ch := range gitx.Lines(ls) {
		if !strings.HasSuffix(ch, "/change.json") {
			continue
		}
		s, err := gitx.Run(d, "show", head+":"+ch)
		var c struct {
			Slug    string   `json:"slug"`
			Status  string   `json:"status"`
			Touches []string `json:"touches"`
			Applied struct {
				Version any `json:"version"`
			} `json:"applied"`
		}
		if err != nil || json.Unmarshal([]byte(s), &c) != nil || c.Status != "applied" || fmt.Sprint(c.Applied.Version) != hv {
			continue
		}
		for _, ac := range t.AcceptanceCriteria {
			for _, x := range c.Touches {
				if x == ac {
					hit += " " + ac + " (change " + c.Slug + ")"
					break
				}
			}
		}
	}
	if hit == "" {
		return ""
	}
	return fmt.Sprintf("the task is pinned to %s, but the approved revision v%s of %s in %s/ changes:%s. The code would be merged against a spec that has moved: run sdkit sdd block --spec-change --reason \"spec %s v%s changes%s\", then sdkit sdd revalidate --sha <the approved revision> once the task is re-pinned",
		short(sha), hv, id, e.specsDir(), hit, id, hv, hit)
}

// mMerged: the PR head recorded at `advance pr` (and the branch's tip, when it is still here) is in the resolved base.
func (e *Env) mMerged(t *Task) string {
	head, br := t.PRHead, t.PRBranch
	if head == "" {
		return "no PR head was recorded at 'pr', so the merge cannot be checked"
	}
	_, _ = gitx.Run(e.Root, "fetch", "-q", "origin") // best effort: an offline check uses the refs it has, and fails closed below
	base := e.Base()
	if gitx.RevParse(e.Root, base) == "" {
		return fmt.Sprintf("the base '%s' is not a known commit (fetch it)", base)
	}
	if _, err := gitx.Run(e.Root, "merge-base", "--is-ancestor", head, base); err != nil {
		if br == "" {
			br = "?"
		}
		return fmt.Sprintf("the PR head %s (%s) is not merged into %s: 'done' comes after the merge, and is never declared", short(head), br, base)
	}
	if br != "" && br != "HEAD" {
		if tip, err := gitx.Run(e.Root, "rev-parse", "-q", "--verify", "refs/heads/"+br); err == nil {
			if _, err := gitx.Run(e.Root, "merge-base", "--is-ancestor", tip, base); err != nil {
				return fmt.Sprintf("%s has commits after the PR head that are not in %s", br, base)
			}
		}
	}
	return ""
}

// writeDoD writes the proposal, its Definition of Done seeded from the contract's acceptance criteria in the dod-check format.
func writeDoD(path string, t jira.Task, scen map[string]string) error {
	key := tstr(t, "key")
	if key == "" {
		key = tstr(t, "externalId")
	}
	k2 := tstr(t, "key")
	if k2 == "" {
		k2 = "?"
	}
	l := []string{"# " + key + ": managed task", "",
		fmt.Sprintf("Generated by sdkit sdd from the Jira task contract of %s (%s): spec %s v%s at %s.", k2, tstr(t, "externalId"), tstr(t, "specId"), tstr(t, "specVersion"), tstr(t, "specSha")),
		"The approved spec is the source of truth: the PRD, architecture and spec are not regenerated here. A spec defect is `sdkit sdd block --spec-change`.",
		"Each condition below is the spec's own scenario for that acceptance criterion; nothing is regenerated. Change a line only when the code shows it cannot be tested as written (then block with a spec change); keep the D ids and a Verified-by method on every line.", "",
		"## Scope", ""}
	if sc := tlist(t, "scope"); len(sc) == 0 {
		l = append(l, "- not set by the task; find the files from the acceptance criteria in the repo")
	} else {
		for _, s := range sc {
			l = append(l, "- "+s)
		}
	}
	l = append(l, "", "## Dependencies", "")
	if d := tlist(t, "dependencies"); len(d) == 0 {
		l = append(l, "- none")
	} else {
		for _, s := range d {
			l = append(l, "- "+s)
		}
	}
	l = append(l, "", "## Definition of Done", "")
	for i, ac := range tlist(t, "acceptanceCriteria") {
		cond := fmt.Sprintf("acceptance criterion %s of spec %s holds", ac, tstr(t, "specId"))
		if sc := scen[ac]; sc != "" {
			cond = ac + ": " + sc
		}
		l = append(l, fmt.Sprintf("- D%d: %s | Verified by: automated test", i+1, cond))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return store.WriteAtomic(path, []byte(strings.Join(l, "\n")+"\n"))
}

// mProposedGate is the managed addition to `advance proposed` (after the standalone checks, which all still apply).
func (e *Env) mProposedGate(t *Task) *Refusal {
	c, why := e.mContract(t.Ticket)
	if why != "" {
		return rej("missing-field", "%s", why)
	}
	if now := tstr(c, "specSha"); now != t.SpecSha {
		return rej("inconsistent", "the task contract of %s now pins %s, not %s that this task was opened on. A spec change goes through sdkit sdd block --spec-change and sdkit sdd revalidate --sha <approved revision>.", t.Ticket, short(now), short(t.SpecSha))
	}
	if why := e.mSha(t.SpecSha); why != "" {
		return rej("missing-artifact", "%s", why)
	}
	if why := e.mDeps(t.Ticket); why != "" {
		return rej("out-of-phase", "%s", why)
	}
	if b, err := os.ReadFile(e.abs(t.Proposal)); err == nil {
		if out, ok := plan.CheckDoD(string(b)); !ok {
			for _, l := range out {
				fmt.Fprintln(e.Err, l)
			}
			return rej("no-dod", "%s: the Definition of Done does not pass dod-check.sh (D<n>: <condition> | Verified by: <method>).", t.Proposal)
		}
	}
	return nil
}

var reScenario = regexp.MustCompile(`(?m)^####[ \t]+Scenario:[ \t]*(AC-[A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*)([^\n]*)`)

// scenarioSummaries maps each active AC id to the one-line text of its scenario (WHEN/THEN lines) in a spec.md, so the
// Definition of Done is seeded from the approved spec and needs no rewriting. "|" is replaced: it separates the Verified-by part.
func scenarioSummaries(text string) map[string]string {
	out := map[string]string{}
	locs := reScenario.FindAllStringSubmatchIndex(text, -1)
	for i, m := range locs {
		if strings.Contains(text[m[4]:m[5]], "withdrawn") {
			continue
		}
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := text[m[1]:end]
		if j := strings.Index(body, "\n#"); j >= 0 {
			body = body[:j]
		}
		var parts []string
		for _, l := range strings.Split(body, "\n") {
			if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-* ")); l != "" {
				parts = append(parts, strings.ReplaceAll(strings.ReplaceAll(l, "**", ""), "|", "/"))
			}
		}
		s := strings.Join(parts, "; ")
		if r := []rune(s); len(r) > 400 {
			s = string(r[:400]) + "..."
		}
		out[text[m[2]:m[3]]] = s
	}
	return out
}

// specScenarios reads the scenarios of spec id at the pinned revision from the specs checkout; nil when anything is unreadable.
func (e *Env) specScenarios(id, sha string) map[string]string {
	d, base := e.specsPath(), e.setting("TASKS_SPEC_PATH", "specs")+"/"+id
	files, err := gitx.Run(d, "ls-tree", "-r", "--name-only", sha, "--", base)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, f := range gitx.Lines(files) {
		if !strings.HasSuffix(f, "/spec.md") || strings.Contains(f, "/changes/") {
			continue
		}
		if txt, err := gitx.Run(d, "show", sha+":"+f); err == nil {
			for k, v := range scenarioSummaries(txt) {
				out[k] = v
			}
		}
	}
	return out
}
