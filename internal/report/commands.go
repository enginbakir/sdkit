package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/usage"
)

// Usage is printed for an unknown command.
const Usage = `usage: sdkit report <command>
  task [slug|latest] [--run ID] [--detailed|--json|--markdown]     the stored report of a finished task, else a live view of an open one
  session [id|latest|current] [--detailed|--json|--markdown]       a session: stored summary, else built now (never stored while it may still run)
  latest [--detailed|--json|--markdown]                            the newest stored report (task or session)
  history [--json]                                                 every stored report, one line each
  finalize --status completed|failed|blocked|partial|aborted [--slug S] [--force] [--quiet]
  session-end [--session ID] [--reason R] [--quiet]                a true end-of-session signal: mark the session ended and store its summary
  recover [--quiet]                                                sessions with records but no end signal get a summary marked "interrupted"
  regenerate task|session ID                                       rebuild a stored report from the records still on disk (the status is kept)
The read commands print on stdout; finalize, session-end and recover print on stderr and always exit 0. VALUE_REPORT=on switches the automatic parts on.`

type opts struct {
	mode                                string
	detail, force, quiet                bool
	slug, run, sid, status, reason, sub string
}

// parse reads the flags of a command; the second result is false (and a message was printed) for an unknown option.
func (e *Env) parse(args []string) (opts, bool) {
	o := opts{mode: "text"}
	for i := 0; i < len(args); i++ {
		a, next := args[i], ""
		if i+1 < len(args) {
			next = args[i+1]
		}
		switch a {
		case "--detailed":
			o.detail = true
		case "--json":
			o.mode = "json"
		case "--markdown":
			o.mode = "markdown"
		case "--force":
			o.force = true
		case "--quiet":
			o.quiet = true
		case "--status", "--slug", "--run", "--session", "--reason":
			i++
			switch a {
			case "--status":
				o.status = next
			case "--slug":
				o.slug = next
			case "--run":
				o.run = next
			case "--session":
				o.sid = next
			case "--reason":
				o.reason = next
			}
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(e.Err, "report: unknown option %s\n", a)
				return o, false
			}
			o.sub = a
		}
	}
	return o, true
}

func (e *Env) fail(code int, f string, a ...any) int {
	fmt.Fprintf(e.Err, f+"\n", a...)
	return code
}

func (e *Env) emit(raw []byte, o opts) int {
	fmt.Fprint(e.Out, show(raw, o.mode, o.detail))
	return 0
}

// Run is `sdkit report ...`; it returns the exit code.
func (e *Env) Run(args []string) int {
	if len(args) == 0 {
		return e.fail(2, "%s", Usage)
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "task":
		return e.cmdTask(args)
	case "session":
		return e.cmdSession(args)
	case "latest":
		o, ok := e.parse(args)
		if !ok {
			return 2
		}
		x := e.latestEntry("")
		if x == nil {
			return e.fail(1, "report: nothing stored yet (reports are written when a task ends: VALUE_REPORT=on)")
		}
		name := strings.TrimSuffix(x.File[strings.Index(x.File, "/")+1:], ".json")
		if j := e.stored(x.Kind, name); len(j) > 0 {
			return e.emit(j, o)
		}
		return 0
	case "history":
		return e.cmdHistory(args)
	case "finalize", "session-end", "recover":
		o, ok := e.parse(args)
		if !ok {
			return 2
		}
		switch cmd {
		case "finalize":
			e.Finalize(o.slug, o.status, o.force, o.quiet)
		case "session-end":
			e.SessionEnd(o.sid, o.reason, o.force, o.quiet)
		default:
			e.Recover(o.sid, o.force, o.quiet)
		}
		return 0
	case "regenerate":
		return e.cmdRegenerate(args)
	}
	return e.fail(2, "%s", Usage)
}

func (e *Env) cmdTask(args []string) int {
	o, ok := e.parse(args)
	if !ok {
		return 2
	}
	s := o.sub
	if s == "" {
		s = "latest"
	}
	var j []byte
	if s == "latest" {
		if x := e.latestEntry("task"); x != nil {
			j = e.stored("task", x.Key)
		} else if s = state.ActiveSlug(e.P); s == "" {
			return e.fail(1, "report: no task report and no open task (set VALUE_REPORT=on, or finish a task)")
		}
	}
	if len(j) == 0 {
		if !safeSlug(s) {
			return e.fail(2, "report: bad task id")
		}
		if o.run != "" {
			j = e.stored("task", s+"@"+o.run)
		} else {
			files, _ := filepath.Glob(filepath.Join(e.rdir(), "task", s+"@*.json"))
			sort.SliceStable(files, func(a, b int) bool { return mtime(files[a]).After(mtime(files[b])) })
			if len(files) == 0 {
				files = []string{filepath.Join(e.rdir(), "task", s+".json")}
			}
			if b, err := os.ReadFile(files[0]); err == nil {
				j = bytes.TrimSpace(b)
			}
		}
		if len(j) == 0 {
			_, err := os.Stat(filepath.Join(e.work(), s, "state.json"))
			if e.src(s, "usage") == "" && e.src(s, "events") == "" && err != nil {
				return e.fail(1, "report: no records for task '%s'", s)
			}
			j = rawOf(e.buildTask(s, "in-progress", false, "live view of a task that has not ended", false))
		}
	}
	return e.emit(j, o)
}

func (e *Env) cmdSession(args []string) int {
	o, ok := e.parse(args)
	if !ok {
		return 2
	}
	s := o.sub
	if s == "" || s == "latest" || s == "current" {
		files, _ := filepath.Glob(filepath.Join(e.work(), "*", "usage.jsonl"))
		a, _ := filepath.Glob(filepath.Join(e.work(), "_archive", "*.usage.jsonl"))
		s, ts := "", ""
		for _, f := range append(files, a...) {
			for _, l := range usage.ReadLines(f) {
				if l.Session != "" && l.TS >= ts {
					s, ts = l.Session, l.TS
				}
			}
		}
		if s == "" {
			return e.fail(1, "report: no ledger row carries a session id yet")
		}
		o.sub = s
	}
	s = o.sub
	if !safeSlug(s) {
		return e.fail(2, "report: bad session id")
	}
	j := e.stored("session", s)
	if len(j) == 0 {
		st, reason, ended := "running", "", (*string)(nil)
		b, _ := os.ReadFile(filepath.Join(e.rdir(), "sessions.jsonl"))
		for _, l := range strings.Split(string(b), "\n") {
			var m struct{ Session, State, Reason, TS string }
			if json.Unmarshal([]byte(l), &m) == nil && m.Session == s {
				st, reason, ended = m.State, m.Reason, ptr(m.TS)
			}
		}
		r, ok := e.buildSession(s, st, reason, ended, false)
		if !ok {
			return e.fail(1, "report: no records for session '%s'", s)
		}
		j = rawOf(r)
	}
	return e.emit(j, o)
}

func (e *Env) cmdHistory(args []string) int {
	o, ok := e.parse(args)
	if !ok {
		return 2
	}
	idx := e.index()
	if _, err := os.Stat(filepath.Join(e.rdir(), "index.jsonl")); err != nil {
		fmt.Fprintln(e.Out, "no stored reports")
		return 0
	}
	sort.SliceStable(idx, func(i, j int) bool { return idx[i].TS < idx[j].TS })
	if o.mode == "json" {
		if idx == nil {
			idx = []Index{}
		}
		b, _ := marshal(idx)
		var out bytes.Buffer
		_ = json.Indent(&out, b, "", "  ")
		fmt.Fprintln(e.Out, out.String())
		return 0
	}
	n := func(p any, d string) string {
		switch v := p.(type) {
		case *int:
			if v != nil {
				return fmt.Sprint(*v)
			}
		case *float64:
			if v != nil {
				return num(*v)
			}
		}
		return d
	}
	var rows [][]string
	for _, x := range idx {
		r := []string{x.TS, x.Kind, x.ID, x.Status}
		if x.Kind == "task" {
			r = append(r, "tokens "+n(x.Tokens, "N/A"), "cost "+n(x.Cost, "N/A"), "saved "+n(x.SavedUSD, "N/A"))
		} else {
			r = append(r, "tasks "+n(x.Tasks, "N/A"))
		}
		rows = append(rows, r)
	}
	w := make([]int, 7)
	for _, r := range rows {
		for i, c := range r {
			if l := len([]rune(c)); l > w[i] {
				w[i] = l
			}
		}
	}
	for _, r := range rows {
		var l []string
		for i, c := range r {
			if i < len(r)-1 {
				c += strings.Repeat(" ", w[i]-len([]rune(c)))
			}
			l = append(l, c)
		}
		fmt.Fprintln(e.Out, strings.Join(l, "  "))
	}
	return 0
}

func (e *Env) cmdRegenerate(args []string) int {
	const u = "usage: sdkit report regenerate task|session ID"
	kind, id := "", ""
	if len(args) > 0 {
		kind = args[0]
	}
	if len(args) > 1 {
		id = args[1]
	}
	switch kind {
	case "task":
		if id == "" || !safeSlug(id) {
			return e.fail(2, "%s", u)
		}
		f := filepath.Join(e.rdir(), "task", id+".json")
		if _, err := os.Stat(f); err != nil {
			files, _ := filepath.Glob(filepath.Join(e.rdir(), "task", id+"@*.json"))
			sort.SliceStable(files, func(a, b int) bool { return mtime(files[a]).After(mtime(files[b])) })
			if len(files) == 0 {
				return e.fail(1, "report: no stored report for '%s'", id)
			}
			f = files[0]
		}
		b, _ := os.ReadFile(f)
		var old TaskReport
		_ = json.Unmarshal(b, &old)
		st := old.Task.StatusRequested
		if st == "" {
			st = old.Task.Status
		}
		r := e.buildTask(old.Task.ID, st, true, "regenerated from the records on disk", true)
		if r.Key != old.Key {
			return e.fail(1, "report: the run id no longer matches the stored report; nothing was changed")
		}
		if r.Tokens.State == "unavailable" && old.Tokens.State != "unavailable" {
			return e.fail(0, "report: the usage records of '%s' are gone (not archived); the stored report is kept as it is", old.Task.ID)
		}
		if e.putTask(r) == nil {
			fmt.Fprintln(e.Out, "regenerated "+r.Key)
		}
	case "session":
		if id == "" || !safeSlug(id) {
			return e.fail(2, "%s", u)
		}
		st := "interrupted"
		if old := e.stored("session", id); len(old) > 0 {
			var s SessionReport
			_ = json.Unmarshal(old, &s)
			st = s.Session.State
		}
		r, ok := e.buildSession(id, st, "regenerated", nil, true)
		if !ok {
			return e.fail(0, "report: no records left for session '%s'; the stored report is kept", id)
		}
		if e.putSession(id, r) == nil {
			fmt.Fprintln(e.Out, "regenerated session "+id)
		}
	default:
		return e.fail(2, "%s", u)
	}
	return 0
}
