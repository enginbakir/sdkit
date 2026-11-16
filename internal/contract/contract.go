// Package contract checks, deterministically and with no model, that a handoff and an agent's final answer are well-formed and
// consistent (docs/design-contracts.md). A valid contract says the exchange is well-formed, never that the work is done or correct:
// it does not touch the phase machine, the verdict, tests or git. Registry: <project-dir>/work/<slug>/contracts/<label>.json.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/store"
)

// Version is the contract version this kit speaks.
const Version = 1

// Options are the flags of issue and check-result.
type Options struct {
	Role, Task, Cwd, WantID string
	CheckOnly, Record       bool   // CheckOnly: validate a handoff without registering it
	LegacyOK                bool   // an answer without the line is fine when no handoff of that role is waiting
	Source                  string // who asked, recorded in the telemetry event (default contract)
}

// Result is one verdict on a handoff or an answer.
type Result struct {
	Valid                   bool
	Silent                  bool // legacy answer: nothing to say
	Stage, ID, Role, Status string
	Reason, Detail          string
}

// Line is the one-line output.
func (r Result) Line() string {
	if r.Silent {
		return ""
	}
	if !r.Valid {
		s := "CONTRACT=invalid stage=" + r.Stage
		if r.ID != "" {
			s += " id=" + r.ID
		}
		if r.Role != "" {
			s += " role=" + r.Role
		}
		return s + " reason=" + r.Reason + ": " + r.Detail
	}
	s := "CONTRACT=valid stage=" + r.Stage + " id=" + r.ID + " role=" + r.Role
	if r.Stage == "result" {
		s += " status=" + r.Status
	}
	return s
}

// Exit: 0 valid (a result: status done), 1 valid but the agent reports partial|blocked|failed, 2 invalid.
func (r Result) Exit() int {
	switch {
	case r.Silent:
		return 0
	case !r.Valid:
		return 2
	case r.Stage == "result" && r.Status != "done":
		return 1
	}
	return 0
}

// C is the contracts of one project.
type C struct{ P project.Project }

// New resolves the project of root.
func New(root string) *C { return &C{P: project.Resolve(root)} }

// Check validates text as stage "handoff" (issue) or "result" (check-result); run-role calls it.
func (c *C) Check(stage, text string, o Options) Result {
	if stage == "handoff" {
		return c.Issue(text, o)
	}
	return c.CheckResult(text, o)
}

// tele is one contract.check event (and a failure event when invalid); off unless TELEMETRY=1 (the sink decides), never changes the answer.
func tele(outcome, stage, id, role, status, reason string, bytes int, src string) {
	if src == "" {
		src = "contract"
	}
	slug, rest, _ := strings.Cut(id, "/")
	if id == "" {
		rest = ""
	} else if !strings.Contains(id, "/") {
		rest = id
	}
	var task []string
	if slug != "" {
		task = []string{"task", slug}
	}
	sdd.Tel.Emit("contract.check", append(task, "outcome", outcome, "stage", stage, "contract", rest, "role", role, "status", status, "reason", reason, "bytes", fmt.Sprint(bytes), "cv", fmt.Sprint(Version), "source", src)...)
	if outcome == "invalid" {
		sdd.Tel.Emit("failure", append(task, "category", "contract", "reason", reason, "role", role, "source", src)...)
	}
}

func (c *C) emit(r Result, text string, o Options) Result {
	if !r.Silent && !(r.Stage == "handoff" && o.CheckOnly) {
		if r.Valid {
			st := ""
			if r.Stage == "result" {
				st = r.Status
			}
			tele("valid", r.Stage, r.ID, r.Role, st, "", len(text), o.Source)
		} else {
			tele("invalid", r.Stage, r.ID, r.Role, r.Status, r.Reason, len(text), o.Source)
		}
	}
	return r
}

type outcome struct {
	Status string `json:"status"`
	Bytes  int    `json:"bytes"`
	At     string `json:"at"`
}

type entry struct {
	V            int      `json:"v"`
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	Access       string   `json:"access"`
	State        string   `json:"state"`
	Issued       string   `json:"issued"`
	Writes       []string `json:"writes"`
	Out          *string  `json:"out"`
	RetryOf      *string  `json:"retry_of"`
	HandoffBytes int      `json:"handoff_bytes"`
	Result       *outcome `json:"result"`
	Reason       *string  `json:"reason"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	Updated      string   `json:"updated,omitempty"`
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func (c *C) dir(slug string) string { return filepath.Join(c.P.Dir(), "work", slug, "contracts") }
func (c *C) file(id string) string {
	return filepath.Join(c.dir(id[:strings.Index(id, "/")]), id[strings.Index(id, "/")+1:]+".json")
}

func (c *C) get(id string) (*entry, bool) {
	if !validID(id) {
		return nil, false
	}
	b, err := os.ReadFile(c.file(id))
	var e entry
	return &e, err == nil && json.Unmarshal(b, &e) == nil
}

// update changes the entry of id under its lock; false when there is none.
func (c *C) update(id string, fn func(*entry)) bool {
	if !validID(id) {
		return false
	}
	f := c.file(id)
	if _, err := os.Stat(f); err != nil {
		return false
	}
	return store.Update(f, func(old []byte) ([]byte, error) {
		var e entry
		if err := json.Unmarshal(old, &e); err != nil {
			return nil, err
		}
		fn(&e)
		return json.Marshal(e)
	}) == nil
}

func (c *C) active(task string) string {
	if task != "" {
		return task
	}
	return state.ActiveSlug(c.P)
}

func validPart(s string) bool {
	return s != "" && s != "." && s != ".." && len(s) <= 64 && state.ValidSlug(s)
}

func validID(id string) bool {
	a, b, ok := strings.Cut(id, "/")
	return ok && validPart(a) && validPart(b)
}

// safeRel: a plain repo-relative path (no absolute, ~, .., .git, glob or odd characters).
func safeRel(p string) bool {
	if p == "" || strings.ContainsAny(p[:1], "/~-") || strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_./@+-") != "" {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." || s == ".git" {
			return false
		}
	}
	return true
}

// inside: the real location of base/p's directory stays under base (symlinks resolved).
func inside(base, p string) bool {
	b, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	d, err := filepath.EvalSymlinks(filepath.Dir(filepath.Join(base, p)))
	if err != nil {
		return false
	}
	return d == b || strings.HasPrefix(d, b+string(filepath.Separator))
}

func inScope(f string, list []string) bool {
	for _, p := range list {
		if p = strings.TrimSuffix(strings.TrimSpace(p), "/"); p != "" && (f == p || strings.HasPrefix(f, p+"/")) {
			return true
		}
	}
	return false
}

func word1(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// scan: the rest of the first line that starts with KEY: for each key of the brief.
func scan(text string) map[string]string {
	want := map[string]bool{"CONTRACT": true, "ID": true, "ROLE": true, "TASK": true, "READ": true, "SCOPE": true, "OUT": true, "WORKTREE": true, "RETRY-OF": true}
	got := map[string]string{}
	seen := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimLeft(l, " \t")
		if i := strings.Index(l, ":"); i > 0 && want[l[:i]] && !seen[l[:i]] {
			seen[l[:i]] = true
			got[l[:i]] = strings.Trim(l[i+1:], " \t\r")
		}
	}
	return got
}

// roleMeta reads access and kind from the front matter of the role file (the kit's, else the project's).
func (c *C) roleMeta(role string) (access, kind string, ok bool) {
	b, err := kit.FS.ReadFile("core/roles/" + role + ".md")
	if err != nil {
		if b, err = os.ReadFile(filepath.Join(c.P.Dir(), "roles", role+".md")); err != nil {
			return "", "", false
		}
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case l == "---":
			if n++; n > 1 {
				return access, kind, true
			}
		case n == 1 && strings.HasPrefix(l, "access:"):
			access = strings.TrimSpace(l[7:])
		case n == 1 && strings.HasPrefix(l, "kind:"):
			kind = strings.TrimSpace(l[5:])
		}
	}
	return access, kind, true
}

func csv(s string) []string { return strings.Split(s, ",") }

// Issue validates a handoff that carries `CONTRACT: 1` and `ID: <slug>/<label>` and registers it.
func (c *C) Issue(text string, o Options) Result { return c.emit(c.issue(text, o), text, o) }

func (c *C) issue(text string, o Options) Result {
	r := Result{Stage: "handoff"}
	bad := func(reason, detail string) Result { r.Reason, r.Detail = reason, detail; return r }
	k := scan(text)
	cv, id, hrole := word1(k["CONTRACT"]), word1(k["ID"]), word1(k["ROLE"])
	r.ID, r.Role = id, hrole
	switch {
	case cv == "":
		return bad("missing-field", "no CONTRACT: line")
	case strings.Trim(cv, "0123456789") != "":
		return bad("malformed", "CONTRACT: must be a number")
	case cv != fmt.Sprint(Version):
		return bad("unsupported-version", fmt.Sprintf("CONTRACT: %s (this kit speaks %d)", cv, Version))
	case id == "":
		return bad("missing-field", "no ID: line")
	case !validID(id):
		return bad("bad-value", "ID must be <task-slug>/<label> (letters, digits, _ . -)")
	}
	slug, _, _ := strings.Cut(id, "/")
	if a := c.active(o.Task); a != "" && a != slug {
		return bad("task-mismatch", fmt.Sprintf("ID names task '%s', the open task is '%s'", slug, a))
	}
	if hrole == "" {
		return bad("missing-field", "no ROLE: line")
	}
	if rl := o.Role[strings.LastIndex(o.Role, ":")+1:]; o.Role != "" && rl != hrole {
		return bad("role-mismatch", fmt.Sprintf("ROLE: %s but the agent being started is '%s'", hrole, rl))
	}
	access, kind, found := c.roleMeta(strings.TrimSuffix(hrole, "-deep"))
	if !found {
		return bad("role-mismatch", fmt.Sprintf("unknown role '%s'", hrole))
	}
	if kind != "subagent" {
		return bad("role-mismatch", fmt.Sprintf("'%s' is not a subagent role", hrole))
	}
	if k["TASK"] == "" {
		return bad("missing-field", "no TASK: line")
	}
	if k["SCOPE"] == "" {
		return bad("missing-field", "no SCOPE: line")
	}
	if !strings.HasPrefix(k["SCOPE"], "writes:") {
		return bad("bad-value", "SCOPE: must start with 'writes:'")
	}
	wl := strings.NewReplacer(" ", "", "\t", "").Replace(strings.TrimPrefix(k["SCOPE"], "writes:"))
	if access == "read-only" {
		if wl != "none" {
			return bad("inconsistent", hrole+" is read-only but SCOPE lists writes")
		}
	} else if wl == "" || wl == "none" {
		return bad("missing-field", hrole+" writes files but SCOPE: writes: is none or empty")
	}
	base := c.P.Root
	if wt := word1(k["WORKTREE"]); wt != "" {
		if st, err := os.Stat(wt); err != nil || !st.IsDir() {
			return bad("missing-artifact", fmt.Sprintf("WORKTREE %s is not a directory", wt))
		}
		base = wt
	}
	if wl != "none" {
		for _, p := range csv(wl) {
			if p = strings.TrimSuffix(p, "/"); !safeRel(p) {
				return bad("unsafe-path", fmt.Sprintf("writes: '%s'", p))
			}
		}
	}
	for _, p := range csv(k["READ"]) {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if !safeRel(p) {
			return bad("unsafe-path", fmt.Sprintf("READ: '%s'", p))
		}
		if _, err := os.Stat(filepath.Join(base, p)); err != nil {
			return bad("missing-artifact", fmt.Sprintf("READ: '%s' does not exist", p))
		}
		if !inside(base, p) {
			return bad("unsafe-path", fmt.Sprintf("READ: '%s' resolves outside the repository", p))
		}
	}
	out := word1(k["OUT"])
	if out != "" {
		out = strings.TrimPrefix(out, c.P.KitDir+"/")
		if !safeRel(out) {
			return bad("unsafe-path", fmt.Sprintf("OUT: '%s'", out))
		}
		if !strings.HasPrefix(out, "work/"+slug+"/out/") {
			return bad("unsafe-path", "OUT: must be under work/"+slug+"/out/")
		}
	}
	retry := word1(k["RETRY-OF"])
	if retry != "" {
		if !strings.Contains(retry, "/") {
			retry = slug + "/" + retry
		}
		if rs, _, _ := strings.Cut(retry, "/"); rs != slug {
			return bad("task-mismatch", "RETRY-OF belongs to another task")
		}
		re, ok := c.get(retry)
		if !ok {
			return bad("unknown-id", fmt.Sprintf("RETRY-OF %s was never issued", retry))
		}
		if re.Role != hrole {
			return bad("role-mismatch", fmt.Sprintf("RETRY-OF %s was issued to another role", retry))
		}
		if s := re.State; s != "invalid" && s != "interrupted" && s != "issued" {
			return bad("bad-value", fmt.Sprintf("RETRY-OF %s is %s, only an invalid or interrupted id can be retried", retry, s))
		}
	}
	if !o.CheckOnly {
		if _, err := os.Stat(c.file(id)); err == nil {
			return bad("duplicate", fmt.Sprintf("ID %s was already issued", id))
		}
		if os.MkdirAll(c.dir(slug), 0o755) != nil {
			return bad("bad-value", "cannot create the registry")
		}
		// first writer wins: the file is created exclusively, so two issues of one id cannot both succeed
		f, err := os.OpenFile(c.file(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return bad("duplicate", fmt.Sprintf("ID %s was already issued", id))
		}
		e := entry{V: Version, ID: id, Role: hrole, Access: access, State: "issued", Issued: now(), Writes: []string{}, HandoffBytes: len(text)}
		if wl != "none" {
			e.Writes = csv(wl)
		}
		if out != "" {
			e.Out = &out
		}
		if retry != "" {
			e.RetryOf = &retry
		}
		b, _ := json.Marshal(e)
		_, werr := f.Write(append(b, '\n'))
		f.Close()
		if werr != nil {
			os.Remove(c.file(id))
			return bad("bad-value", "cannot write the registry")
		}
		if retry != "" {
			c.update(retry, func(e *entry) { e.State, e.SupersededBy = "superseded", id })
		}
	}
	r.Valid = true
	return r
}

// CheckResult validates the single `HANDOFF-RESULT/1 | id: .. | role: .. | status: .. | artifacts: .. | blockers: .. | next: ..` line.
func (c *C) CheckResult(text string, o Options) Result {
	return c.emit(c.checkResult(text, o), text, o)
}

func (c *C) checkResult(text string, o Options) Result {
	r := Result{Stage: "result", ID: o.WantID, Role: o.Role[strings.LastIndex(o.Role, ":")+1:]}
	role, cwd := r.Role, o.Cwd
	if cwd == "" {
		cwd = c.P.Root
	}
	bad := func(reason, detail string) Result {
		r.Reason, r.Detail = reason, detail
		if o.Record && validID(r.ID) {
			c.update(r.ID, func(e *entry) {
				if e.State == "issued" || e.State == "invalid" {
					e.State, e.Reason, e.Updated = "invalid", &reason, now()
				}
			})
		}
		return r
	}
	var marks []string
	seen := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(strings.TrimLeft(l, " \t>`*-"), " \t`*\r")
		if strings.HasPrefix(l, "HANDOFF-RESULT") && !seen[l] {
			seen[l] = true
			marks = append(marks, l)
		}
	}
	if len(marks) == 0 {
		if o.LegacyOK && len(c.Pending(role, o.Task)) == 0 {
			r.Valid, r.Silent = true, true
			return r
		}
		if strings.TrimSpace(text) != "" {
			return bad("malformed", "no HANDOFF-RESULT line in the answer")
		}
		return bad("no-result", "the agent returned nothing")
	}
	if len(marks) != 1 {
		return bad("malformed", fmt.Sprintf("%d different HANDOFF-RESULT lines (exactly one is allowed)", len(marks)))
	}
	head, rest, hasRest := strings.Cut(marks[0], "|")
	ver, isV := strings.CutPrefix(strings.TrimSpace(head), "HANDOFF-RESULT/")
	if !isV || ver == "" || ver[0] < '0' || ver[0] > '9' {
		return bad("malformed", "first field must be HANDOFF-RESULT/<version>")
	}
	if strings.Trim(ver, "0123456789") != "" {
		return bad("malformed", fmt.Sprintf("version '%s' is not a number", ver))
	}
	if ver != fmt.Sprint(Version) {
		return bad("unsupported-version", fmt.Sprintf("HANDOFF-RESULT/%s (this kit speaks %d)", ver, Version))
	}
	if !hasRest {
		return bad("missing-field", "no fields after the version")
	}
	f := map[string]string{}
	for _, l := range strings.Split(rest, "|") {
		if l = strings.TrimSpace(l); l == "" {
			return bad("malformed", "empty field (a '|' inside a value?)")
		}
		key, val, _ := strings.Cut(l, ":")
		switch key {
		case "id", "role", "status", "artifacts", "blockers", "next":
		default:
			return bad("malformed", "unknown or unreadable field: "+key)
		}
		if _, dup := f[key]; dup {
			return bad("malformed", fmt.Sprintf("field '%s' appears twice", key))
		}
		f[key] = strings.TrimSpace(val)
	}
	id, rrole, status, arts, blockers := f["id"], f["role"], f["status"], f["artifacts"], f["blockers"]
	for _, k := range []struct{ n, v, hint string }{{"id", id, ""}, {"role", rrole, ""}, {"status", status, ""}, {"artifacts", arts, " (use: artifacts: none)"}, {"blockers", blockers, " (use: blockers: none)"}} {
		if k.v == "" {
			return bad("missing-field", fmt.Sprintf("field '%s' is empty or missing%s", k.n, k.hint))
		}
	}
	r.ID, r.Role = id, rrole
	if !validID(id) {
		return bad("bad-value", "id must be <task-slug>/<label>")
	}
	switch status {
	case "done", "partial", "blocked", "failed":
		r.Status = status
	default:
		return bad("bad-value", fmt.Sprintf("status must be done|partial|blocked|failed, got '%s'", status))
	}
	if len(blockers) > 200 || len(f["next"]) > 200 {
		return bad("bad-value", "blockers and next are limited to 200 characters (put the long form in OUT)")
	}
	slug := id[:strings.Index(id, "/")]
	switch {
	case o.WantID != "" && o.WantID != id:
		return bad("id-mismatch", fmt.Sprintf("the answer says id %s, the handoff was %s", id, o.WantID))
	case c.active(o.Task) != "" && c.active(o.Task) != slug:
		return bad("task-mismatch", fmt.Sprintf("id names task '%s', the open task is '%s'", slug, c.active(o.Task)))
	case role != "" && role != rrole:
		return bad("role-mismatch", fmt.Sprintf("the answer says role %s, the agent is %s", rrole, role))
	}
	e, ok := c.get(id)
	if !ok {
		return bad("unknown-id", id+" was never issued (no registered handoff)")
	}
	if e.Role != rrole {
		return bad("role-mismatch", fmt.Sprintf("the handoff was issued to %s, the answer says %s", e.Role, rrole))
	}
	switch e.State {
	case "accepted":
		return bad("duplicate", id+" already has an accepted result")
	case "superseded":
		return bad("stale", id+" was superseded by "+e.SupersededBy)
	case "interrupted":
		return bad("stale", id+" was closed as interrupted")
	}
	if blockers == "none" && status != "done" {
		return bad("inconsistent", fmt.Sprintf("status %s needs blockers: <why>", status))
	}
	if blockers != "none" && status == "done" {
		return bad("inconsistent", "status done with blockers")
	}
	if arts != "none" {
		if e.Access == "read-only" {
			return bad("inconsistent", rrole+" is read-only and cannot produce artifacts (use artifacts: none)")
		}
		out := ""
		if e.Out != nil {
			out = *e.Out
		}
		for _, p := range csv(arts) {
			p = strings.TrimSuffix(strings.TrimSpace(p), "/")
			if !safeRel(p) {
				return bad("unsafe-path", fmt.Sprintf("artifact '%s'", p))
			}
			if _, err := os.Stat(filepath.Join(cwd, p)); err != nil {
				return bad("missing-artifact", fmt.Sprintf("artifact '%s' does not exist", p))
			}
			if !inside(cwd, p) {
				return bad("unsafe-path", fmt.Sprintf("artifact '%s' resolves outside the repository", p))
			}
			if !inScope(p, e.Writes) && (out == "" || (p != out && p != c.P.KitDir+"/"+out)) {
				return bad("out-of-scope", fmt.Sprintf("artifact '%s' is outside the handoff's writes", p))
			}
		}
	}
	if o.Record {
		// check and set under one lock: of several identical answers exactly one is accepted
		dup := false
		c.update(id, func(e *entry) {
			if dup = e.State == "accepted"; dup {
				return
			}
			e.State, e.Reason = "accepted", nil
			e.Result = &outcome{status, len(text), now()}
		})
		if dup {
			return bad("duplicate", id+" already has an accepted result")
		}
	}
	r.Valid = true
	return r
}

// Pending lists the ids issued and not answered yet (of role, when given) for the task (the open one when empty).
func (c *C) Pending(role, task string) []string {
	role = role[strings.LastIndex(role, ":")+1:]
	s := c.active(task)
	if !validPart(s) {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(c.dir(s), "*.json"))
	var ids []string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var e entry
		if json.Unmarshal(b, &e) == nil && e.State == "issued" && (role == "" || e.Role == role) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// Close marks an id the agent never answered as interrupted (it can only be retried) and returns its state.
func (c *C) Close(id, reason string) (string, bool) {
	if !c.update(id, func(e *entry) {
		if e.State == "issued" || e.State == "invalid" {
			e.State, e.Reason = "interrupted", &reason
		}
	}) {
		return "", false
	}
	e, _ := c.get(id)
	tele("invalid", "result", id, e.Role, "", "no-result", 0, "")
	return e.State, true
}

// Status is one line per id of the task: state, role, status, reason.
func (c *C) Status(slug string) []string {
	files, _ := filepath.Glob(filepath.Join(c.dir(slug), "*.json"))
	var out []string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var e entry
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		l := fmt.Sprintf("%s  %s  %s", e.ID, e.Role, e.State)
		if e.Result != nil {
			l += "  status=" + e.Result.Status
		}
		if e.Reason != nil {
			l += "  reason=" + *e.Reason
		}
		out = append(out, l)
	}
	return out
}

// ID is the ID of a handoff ("" when it has none).
func ID(text string) string { return word1(scan(text)["ID"]) }

// Active is the open task of the project.
func (c *C) Active(task string) string { return c.active(task) }
