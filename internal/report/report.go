package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/store"
	"github.com/wecii/sdkit/internal/telemetry"
	"github.com/wecii/sdkit/internal/usage"
)

// Env is one project the report reads and the places it writes to.
type Env struct {
	Top      string
	P        project.Project
	Get      func(string) string // a setting: sdkit.env, sdkit.local.env, then the environment
	Out, Err io.Writer
	Now      func() time.Time
}

// New opens the project at root.
func New(root string) *Env {
	s, p, _ := project.Load(root)
	return &Env{Top: root, P: p, Get: base.Getter(s.Raw), Out: os.Stdout, Err: os.Stderr, Now: time.Now}
}

// To sends both the output and the stderr text of the report to w.
func (e *Env) To(w io.Writer) *Env { e.Out, e.Err = w, w; return e }

func (e *Env) now() string  { return e.Now().UTC().Format("2006-01-02T15:04:05Z") }
func (e *Env) work() string { return filepath.Join(e.P.Dir(), "work") }
func (e *Env) rdir() string { return filepath.Join(e.work(), "_reports") }

var slugRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

func safeSlug(s string) bool { return slugRe.MatchString(s) }

// src is the live file of a task, else its archived copy, else "".
func (e *Env) src(slug, name string) string {
	for _, f := range []string{filepath.Join(e.work(), slug, name+".jsonl"), filepath.Join(e.work(), "_archive", slug+"."+name+".jsonl")} {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

func slurp(file string) []Rec {
	b, _ := os.ReadFile(file)
	var out []Rec
	for _, l := range strings.Split(string(b), "\n") {
		var m Rec
		if json.Unmarshal([]byte(l), &m) == nil && m != nil {
			out = append(out, m)
		}
	}
	return out
}

var modelRe = regexp.MustCompile(`[^A-Za-z0-9._:/@-]`)

func (e *Env) baselineModel() string {
	return modelRe.ReplaceAllString(e.Get("REPORT_BASELINE_MODEL"), "")
}

// purposes maps a role to the first sentence of its definition (core/roles of the kit); nothing is invented for other roles.
func purposes() map[string]string {
	out := map[string]string{}
	es, _ := kit.FS.ReadDir("core/roles")
	for _, f := range es {
		name := strings.TrimSuffix(f.Name(), ".md")
		if name == "README" || name == f.Name() {
			continue
		}
		b, err := kit.FS.ReadFile("core/roles/" + f.Name())
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(l, "description:") {
				continue
			}
			d := strings.TrimLeft(strings.TrimPrefix(l, "description:"), " \t")
			if i := strings.Index(d, ". "); i >= 0 {
				d = d[:i]
			}
			d = strings.TrimSuffix(d, ".")
			if r := []rune(d); len(r) > 110 {
				d = string(r[:110])
				if i := strings.LastIndex(d, " "); i >= 0 {
					d = d[:i]
				}
				d += "…"
			}
			out[name] = d
			break
		}
	}
	return out
}

// history is the role statistics of the newest 20 stored task reports other than key: the comparable-run baselines.
func (e *Env) history(key string) [][]TaskRole {
	files, _ := filepath.Glob(filepath.Join(e.rdir(), "task", "*.json"))
	sort.SliceStable(files, func(i, j int) bool { return mtime(files[i]).After(mtime(files[j])) })
	if len(files) > 20 {
		files = files[:20]
	}
	var out [][]TaskRole
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var r TaskReport
		if json.Unmarshal(b, &r) != nil || r.Kind != "task" || r.Key == key {
			continue
		}
		var rs []TaskRole
		for _, x := range r.Roles {
			if x.Kind != "orchestrator" && x.Tokens != nil {
				rs = append(rs, x)
			}
		}
		out = append(out, rs)
	}
	return out
}

func mtime(f string) time.Time {
	st, err := os.Stat(f)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// filesChanged is git diff against the base plus untracked files, outside the project directory; nil when there is no base or no change.
func (e *Env) filesChanged() *FilesChanged {
	b := base.Resolve(e.Top, e.Get)
	if b == "" {
		return nil
	}
	seen := map[string]bool{}
	if out, err := gitx.Run(e.Top, "diff", "--name-only", b); err == nil {
		for _, l := range gitx.Lines(out) {
			seen[l] = true
		}
	}
	if out, err := gitx.Run(e.Top, "ls-files", "-o", "--exclude-standard"); err == nil {
		for _, l := range gitx.Lines(out) {
			seen[l] = true
		}
	}
	n := 0
	for l := range seen {
		if l != "" && !strings.HasPrefix(l, e.P.KitDir+"/") {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return &FilesChanged{N: n, Method: "git diff vs " + b + " plus untracked"}
}

// usageFor is the canonical usage report of a task (slug) or of a session (across every task), priced also on the baseline model.
func (e *Env) usageFor(slug, session string) *usage.Result {
	w := e.work()
	prices := os.Getenv("TELEMETRY_PRICES")
	if prices == "" {
		prices = filepath.Join(e.P.Dir(), "pricing.json")
	}
	o := usage.Options{Prices: prices, Session: session, AsModel: e.baselineModel()}
	var files []string
	if slug != "" {
		files, o.Scope = []string{filepath.Join(w, slug, "usage.jsonl"), filepath.Join(w, "_archive", slug+".usage.jsonl")}, "task "+slug+" (with archive)"
		o.Spawns = usage.Spawns(e.P, slug)
	} else {
		files, _ = filepath.Glob(filepath.Join(w, "*", "usage.jsonl"))
		a, _ := filepath.Glob(filepath.Join(w, "_archive", "*.usage.jsonl"))
		files, o.Scope = append(files, a...), "project: open and archived tasks"
	}
	return usage.Compute(files, o)
}

func (e *Env) openState() (task, profile, risk, startedAt string) {
	b, err := os.ReadFile(filepath.Join(e.P.Dir(), ".sdd", "state.json"))
	if err != nil {
		return
	}
	var s struct{ Task, Profile, Risk, StartedAt string }
	_ = json.Unmarshal(b, &s)
	return s.Task, s.Profile, s.Risk, s.StartedAt
}

var runChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// buildTask reads the records of a task and computes its report.
func (e *Env) buildTask(slug, status string, terminal bool, ssrc string, regen bool) *TaskReport {
	m := Meta{Slug: slug, Status: status, Terminal: terminal, StatusSource: ssrc, Now: e.now(), BaselineModel: e.baselineModel(), Regenerated: regen}
	m.BaseMissing = m.BaselineModel == ""
	run := ""
	if b, err := os.ReadFile(filepath.Join(e.work(), slug, "run.id")); err == nil {
		run = runChars.ReplaceAllString(string(b), "")
		if len(run) > 60 {
			run = run[:60]
		}
	}
	var t *telemetry.Summary
	if ev := e.src(slug, "events"); ev != "" {
		if st, err := os.Stat(ev); err == nil && st.Size() > 0 {
			if s, err := telemetry.Summarize(ev, run); err == nil {
				t = s
				if run == "" {
					run = s.Run
				}
			}
		}
	}
	t0 := ""
	if task, prof, risk, started := e.openState(); task == slug {
		m.Profile, m.Risk, t0 = strp(prof), strp(risk), started
		if st, ok := parseISO(started); ok {
			m.WallS = ptr(e.Now().UTC().Sub(st).Seconds())
			*m.WallS = float64(int64(*m.WallS))
		}
	}
	if run == "" {
		run = t0
	}
	if run = runChars.ReplaceAllString(run, ""); run == "" {
		run = "0"
	}
	m.Run, m.Key = run, slug+"@"+run
	usage.Reconcile(e.P, e.Top, os.Getenv("CLAUDE_TRANSCRIPT_DIR"), slug)
	u := e.usageFor(slug, "")
	if u != nil {
		seen := map[string]bool{}
		for _, r := range u.Rows {
			if r.Session != "" && !seen[r.Session] {
				seen[r.Session] = true
				m.Sessions = append(m.Sessions, r.Session)
			}
		}
		sort.Strings(m.Sessions)
	}
	m.Files, m.Purposes = e.filesChanged(), purposes()
	return buildTask(TaskInputs{U: u, T: t, RT: slurp(e.src(slug, "routing")), CX: slurp(e.src(slug, "context")), SL: slurp(e.src(slug, "selection")), Hist: e.history(m.Key), M: m})
}

func (e *Env) tasksOf(session string) []Index {
	var out []Index
	for _, x := range e.index() {
		for _, s := range x.Sessions {
			if x.Kind == "task" && s == session {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

func (e *Env) buildSession(id, state, reason string, ended *string, regen bool) (*SessionReport, bool) {
	u := e.usageFor("", id)
	if u == nil {
		return nil, false
	}
	return buildSession(SessionInputs{U: u, Tasks: e.tasksOf(id), ID: id, State: state, Reason: reason, Now: e.now(), BaselineModel: e.baselineModel(), EndedAt: ended, Key: "session:" + id, Regenerated: regen}), true
}

func (e *Env) index() []Index {
	var out []Index
	b, _ := os.ReadFile(filepath.Join(e.rdir(), "index.jsonl"))
	for _, l := range strings.Split(string(b), "\n") {
		var x Index
		if json.Unmarshal([]byte(l), &x) == nil && x.Key != "" {
			out = append(out, x)
		}
	}
	return out
}

func (e *Env) latestEntry(kind string) *Index {
	idx := e.index()
	sort.SliceStable(idx, func(i, j int) bool { return idx[i].TS < idx[j].TS })
	for i := len(idx) - 1; i >= 0; i-- {
		if kind == "" || idx[i].Kind == kind {
			return &idx[i]
		}
	}
	return nil
}

func indexOf(r *TaskReport, file string) Index {
	x := Index{Key: r.Key, Kind: "task", ID: r.Task.ID, Task: r.Task.ID, Status: r.Task.Status, TS: r.GeneratedAt, File: file, Sessions: orEmpty(r.Task.Sessions), Tokens: r.Tokens.Fresh,
		Cost: r.Cost.APIEquivalent, ByClass: r.Savings.Avoided.ByClass, Gates: r.Task.QualityGates.State, Optims: []string{}}
	if r.Savings.Cost.State != "na" {
		x.SavedUSD = ptr(r.Savings.Cost.SavedUSD)
	}
	if r.Savings.Tokens.State != "na" {
		x.SavedTokens = ptr(r.Savings.Tokens.SavedTokens)
	}
	for _, c := range r.Savings.Cost.Components {
		if c.Name == "avoided executions" {
			x.SavedAvoid += c.USD
		}
	}
	if p := r.Task.Tests.Passed; p != nil {
		x.TestsPassed, x.TestsFailed = *p, *r.Task.Tests.Failed
	}
	for _, o := range r.Optimizations {
		x.Optims = append(x.Optims, o.Name)
	}
	return x
}

// put writes a report atomically, then its index line (an unchanged key is replaced, so the index stays one line per report).
func (e *Env) put(kind, name string, rep any, key string, x Index) error {
	f := filepath.Join(e.rdir(), kind, name+".json")
	b, err := marshal(rep)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	if err := store.WriteAtomic(f, append(b, '\n')); err != nil {
		return err
	}
	ip := filepath.Join(e.rdir(), "index.jsonl")
	unlock, err := store.Lock(ip)
	if err != nil {
		return err
	}
	defer unlock()
	var keep bytes.Buffer
	for _, o := range e.index() {
		if o.Key != key {
			l, _ := marshal(o)
			keep.Write(append(l, '\n'))
		}
	}
	l, _ := marshal(x)
	keep.Write(append(l, '\n'))
	return store.WriteAtomic(ip, keep.Bytes())
}

func (e *Env) putTask(r *TaskReport) error {
	return e.put("task", r.Key, r, r.Key, indexOf(r, "task/"+r.Key+".json"))
}

func (e *Env) putSession(id string, r *SessionReport) error {
	n := len(r.Tasks.List)
	return e.put("session", id, r, r.Key, Index{Key: r.Key, Kind: "session", ID: r.Session.ID, Status: r.Session.State, TS: r.GeneratedAt, File: "session/" + id + ".json", Tasks: &n})
}

func (e *Env) stored(kind, name string) []byte {
	b, err := os.ReadFile(filepath.Join(e.rdir(), kind, name+".json"))
	if err != nil {
		return nil
	}
	return bytes.TrimSpace(b)
}

// show renders a report (raw JSON) as text, markdown or, for json, as indented JSON.
func show(raw []byte, mode string, detail bool) string {
	if mode == "json" {
		var b bytes.Buffer
		if json.Indent(&b, raw, "", "  ") != nil {
			return ""
		}
		return b.String() + "\n"
	}
	var k struct{ Kind string }
	_ = json.Unmarshal(raw, &k)
	var l []string
	if k.Kind == "session" {
		var r SessionReport
		if json.Unmarshal(raw, &r) != nil {
			return ""
		}
		if mode == "markdown" || detail {
			l = r.markdown()
		} else {
			l = r.text()
		}
	} else {
		var r TaskReport
		if json.Unmarshal(raw, &r) != nil {
			return ""
		}
		switch {
		case mode == "markdown":
			l = r.markdown()
		case detail:
			l = r.detailed()
		default:
			l = r.text()
		}
	}
	return strings.Join(l, "\n") + "\n"
}

func rawOf(rep any) []byte { b, _ := marshal(rep); return b }

// export, with REPORT_EXPORT=on, leaves a readable copy of the report in <project-dir>/reports (committed with the project).
func (e *Env) export(id string, raw []byte) {
	if e.Get("REPORT_EXPORT") != "on" || !safeSlug(id) {
		return
	}
	d := filepath.Join(e.P.Dir(), "reports")
	if os.MkdirAll(d, 0o755) != nil {
		return
	}
	if j := show(raw, "json", false); j != "" {
		_ = store.WriteAtomic(filepath.Join(d, id+".json"), []byte(j))
	}
	if md := show(raw, "markdown", true); md != "" {
		_ = store.WriteAtomic(filepath.Join(d, id+".md"), []byte(md))
	}
}

// Finalize writes the report of a task ONCE (a second call finds it and does nothing; force rewrites) and prints it compactly on Err.
// Off unless VALUE_REPORT=on or force. A problem here never reaches the caller.
func (e *Env) Finalize(slug, status string, force, quiet bool) {
	defer func() { _ = recover() }()
	if e.Get("VALUE_REPORT") != "on" && !force {
		return
	}
	switch status {
	case "completed", "failed", "blocked", "partial", "aborted":
	default:
		fmt.Fprintln(e.Err, "report: --status must be completed|failed|blocked|partial|aborted")
		return
	}
	if slug == "" {
		slug = state.ActiveSlug(e.P)
	}
	if slug == "" || !safeSlug(slug) {
		return
	}
	r := e.buildTask(slug, status, true, "terminal state reported by the workflow ("+status+")", false)
	if _, err := os.Stat(filepath.Join(e.rdir(), "task", r.Key+".json")); err == nil && !force {
		e.export(slug, e.stored("task", r.Key))
		if !quiet {
			fmt.Fprintf(e.Err, "SDKit Value Report for %s already exists: sdkit report task %s\n", r.Key, slug)
		}
		return
	}
	if e.putTask(r) != nil {
		return
	}
	e.export(slug, rawOf(r))
	if !quiet {
		fmt.Fprint(e.Err, show(rawOf(r), "text", false))
	}
}

// Rep is the sdd.Reporter of the binary: the report of the task that is ending, once, on stderr, off unless VALUE_REPORT=on.
type Rep struct{}

// Finalize implements sdd.Reporter.
func (Rep) Finalize(status string) {
	defer func() { _ = recover() }()
	e := New(sdd.Root())
	if e.Get("VALUE_REPORT") != "on" {
		return
	}
	task, _, _, _ := e.openState()
	e.Finalize(task, status, false, false)
}

// SessionEnd marks a session ended (a true end-of-session signal) and stores its summary.
func (e *Env) SessionEnd(id, reason string, force, quiet bool) {
	defer func() { _ = recover() }()
	if e.Get("VALUE_REPORT") != "on" && !force {
		return
	}
	if id == "" || !safeSlug(id) || os.MkdirAll(e.rdir(), 0o755) != nil {
		return
	}
	sp := filepath.Join(e.rdir(), "sessions.jsonl")
	if unlock, err := store.Lock(sp); err == nil {
		l, _ := marshal(om{{"session", id}, {"state", "ended"}, {"reason", reason}, {"ts", e.now()}})
		_ = store.AppendLine(sp, string(l))
		unlock()
	}
	r, ok := e.buildSession(id, "ended", reason, ptr(e.now()), false)
	if !ok || e.putSession(id, r) != nil {
		return
	}
	if len(r.Tasks.List) == 0 {
		e.export(id, rawOf(r))
	}
	if !quiet {
		fmt.Fprint(e.Err, show(rawOf(r), "text", false))
	}
}

// Recover stores a summary marked "interrupted" for every earlier session that left records but never got an end signal. The newest
// session may still be running, so it is left alone unless cur names the current one.
func (e *Env) Recover(cur string, force, quiet bool) {
	defer func() { _ = recover() }()
	if e.Get("VALUE_REPORT") != "on" && !force {
		return
	}
	last := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(e.work(), "*", "usage.jsonl"))
	a, _ := filepath.Glob(filepath.Join(e.work(), "_archive", "*.usage.jsonl"))
	for _, f := range append(files, a...) {
		for _, l := range usage.ReadLines(f) {
			if l.Session != "" && l.TS > last[l.Session] {
				last[l.Session] = l.TS
			}
		}
	}
	var all []string
	for s := range last {
		all = append(all, s)
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		return last[all[i]] < last[all[j]] || (last[all[i]] == last[all[j]] && all[i] < all[j])
	})
	newest, n := all[len(all)-1], 0
	for _, s := range all {
		if !safeSlug(s) || s == cur || (s == newest && cur == "") {
			continue
		}
		if _, err := os.Stat(filepath.Join(e.rdir(), "session", s+".json")); err == nil {
			continue
		}
		r, ok := e.buildSession(s, "interrupted", "no end signal", nil, false)
		if !ok || e.putSession(s, r) != nil {
			continue
		}
		if len(r.Tasks.List) == 0 {
			e.export(s, rawOf(r))
		}
		n++
	}
	if n > 0 && !quiet {
		fmt.Fprintf(e.Err, "SDKit: %d earlier session(s) ended without a signal; their summaries were recovered (interrupted): sdkit report history\n", n)
	}
}
