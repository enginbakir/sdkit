package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

// ModelTotals is the usage of one model inside a transcript (a session that switches model is priced per model).
type ModelTotals struct {
	Model      string `json:"model"`
	In         int    `json:"in"`
	Out        int    `json:"out"`
	CacheRead  int    `json:"cache_read"`
	CacheWrite int    `json:"cache_write"`
}

// Totals is the token usage of one Claude Code transcript. In is already without cache.
type Totals struct {
	Messages   int           `json:"messages"`
	Session    string        `json:"session"`
	Provider   string        `json:"provider"`
	FirstTS    *string       `json:"first_ts"`
	LastTS     *string       `json:"last_ts"`
	In         int           `json:"in"`
	Out        int           `json:"out"`
	CacheRead  int           `json:"cache_read"`
	CacheWrite int           `json:"cache_write"`
	Model      string        `json:"model"`
	ByModel    []ModelTotals `json:"by_model"`
}

// records reads a JSONL transcript; a line that is not JSON is skipped. The format is the Claude Code transcript only; the path comes
// from the hook input (or the project's transcript directory) and is opened as given, so a Windows path in either separator form works.
func records(path string) ([]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []any
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var v any
			if json.Unmarshal(line, &v) == nil {
				out = append(out, v)
			}
		}
		if err != nil {
			return out, nil
		}
	}
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string         { s, _ := v.(string); return s }
func iv(v any) int             { f, _ := v.(float64); return int(f) }

var frac = regexp.MustCompile(`\.[0-9]+`)

// first returns the first present non-null value (jq's //).
func first(vs ...any) any {
	for _, v := range vs {
		if v != nil && v != false {
			return v
		}
	}
	return nil
}

// Extract is the one parser of transcript usage. A streamed message appears several times with a growing output_tokens: each field is the
// maximum over the repeats. Records are keyed by requestId, else message id, else uuid, else position; synthetic messages count nothing.
// With since (ISO UTC) only requests at or after it count; a request without a timestamp is kept (it cannot be placed).
func Extract(path, since string) (*Totals, error) {
	recs, err := records(path)
	if err != nil {
		return nil, err
	}
	type msg struct {
		model, k        string
		in, out, cr, cw int
	}
	groups := map[string]*msg{}
	var ts []string
	t := &Totals{ByModel: []ModelTotals{}}
	for i, r := range recs {
		m := obj(r)
		if m == nil {
			continue
		}
		if s, ok := m["timestamp"].(string); ok {
			ts = append(ts, s)
		}
		if t.Session == "" {
			t.Session = str(m["sessionId"])
		}
		if str(m["type"]) != "assistant" {
			continue
		}
		mm := obj(m["message"])
		if id := str(mm["id"]); t.Provider == "" && id != "" {
			switch {
			case strings.HasPrefix(id, "msg_bdrk_"):
				t.Provider = "bedrock"
			case strings.HasPrefix(id, "msg_vrtx_"):
				t.Provider = "vertex"
			case strings.HasPrefix(id, "msg_"):
				t.Provider = "anthropic"
			}
			if t.Provider == "" {
				t.Provider = "-"
			}
		}
		u := obj(mm["usage"])
		model := str(mm["model"])
		if u == nil || model == "<synthetic>" {
			continue
		}
		if s, ok := m["timestamp"].(string); ok && since != "" && frac.ReplaceAllString(s, "") < since {
			continue
		}
		k := str(first(m["requestId"], mm["id"], m["uuid"]))
		if first(m["requestId"], mm["id"], m["uuid"]) == nil {
			k = "line" + itoa(i)
		}
		g := groups[k]
		if g == nil {
			g = &msg{k: k}
			groups[k] = g
		}
		if model != "" {
			g.model = model
		}
		g.in, g.out = max(g.in, iv(u["input_tokens"])), max(g.out, iv(u["output_tokens"]))
		g.cr, g.cw = max(g.cr, iv(u["cache_read_input_tokens"])), max(g.cw, iv(u["cache_creation_input_tokens"]))
	}
	if t.Provider == "-" {
		t.Provider = ""
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	bm := map[string]*ModelTotals{}
	for _, k := range keys {
		g := groups[k]
		t.Messages++
		t.In, t.Out, t.CacheRead, t.CacheWrite = t.In+g.in, t.Out+g.out, t.CacheRead+g.cr, t.CacheWrite+g.cw
		if g.model != "" {
			t.Model = g.model
		}
		b := bm[g.model]
		if b == nil {
			b = &ModelTotals{Model: g.model}
			bm[g.model] = b
		}
		b.In, b.Out, b.CacheRead, b.CacheWrite = b.In+g.in, b.Out+g.out, b.CacheRead+g.cr, b.CacheWrite+g.cw
	}
	ms := make([]string, 0, len(bm))
	for k := range bm {
		ms = append(ms, k)
	}
	sort.Strings(ms)
	for _, k := range ms {
		t.ByModel = append(t.ByModel, *bm[k])
	}
	if len(ts) > 0 {
		sort.Strings(ts)
		t.FirstTS, t.LastTS = &ts[0], &ts[len(ts)-1]
	}
	return t, nil
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// CurrentSlug is the task the ledger belongs to: the open task, else the newest work state, else _misc.
func CurrentSlug(p project.Project) string {
	if s := state.ActiveSlug(p); s != "" {
		return s
	}
	return "_misc"
}

// Window is the start of the task (ISO UTC) in the phase machine's state, or "": where its share of a cumulative session transcript begins.
func Window(p project.Project, slug string) string {
	b, err := os.ReadFile(filepath.Join(p.Dir(), ".sdd", "state.json"))
	if err != nil {
		return ""
	}
	var f struct{ Task, StartedAt string }
	if json.Unmarshal(b, &f) != nil || f.Task != slug {
		return ""
	}
	return f.StartedAt
}

// LedgerFile is work/<slug>/usage.jsonl.
func LedgerFile(p project.Project, slug string) string {
	return filepath.Join(p.Dir(), "work", slug, "usage.jsonl")
}

// TranscriptDir is where Claude Code keeps this project's sessions (the path with every non-alphanumeric byte replaced by "-", as on
// Windows too: the native path is mangled, not a converted one).
func TranscriptDir(top string) string {
	if d := os.Getenv("CLAUDE_TRANSCRIPT_DIR"); d != "" {
		return d
	}
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		h, _ := os.UserHomeDir()
		cfg = filepath.Join(h, ".claude")
	}
	b := []byte(top)
	for i, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			b[i] = '-'
		}
	}
	return filepath.Join(cfg, "projects", string(b))
}

// observe logs one ledger line per model of a transcript (origin observed). The ids are the hooks' ids, so a run the hook already recorded
// is not counted twice. It returns the number of lines.
func observe(file string, j *Totals, id, role, session, kind, source string, stripFrac bool) int {
	ms := j.ByModel
	if len(ms) == 0 {
		ms = []ModelTotals{{}}
	}
	for _, m := range ms {
		ts := Now()
		if j.LastTS != nil {
			ts = *j.LastTS
			if stripFrac {
				ts = frac.ReplaceAllString(ts, "")
			}
		}
		_ = Log(file, Line{V: 2, TS: ts, Role: role, Host: "claude", Model: m.Model, Provider: j.Provider, Session: session, ID: id, Origin: "observed", Kind: kind,
			Source: source, In: Num(m.In), Out: Num(m.Out), CacheRead: Num(m.CacheRead), CacheWrite: Num(m.CacheWrite)})
	}
	return len(ms)
}

// agentRole is the role named by the .meta.json next to a subagent transcript ("subagent" when it names none).
func agentRole(a string) (role string, finished bool) {
	b, _ := os.ReadFile(strings.TrimSuffix(a, ".jsonl") + ".meta.json")
	var m struct {
		AgentType string
		Finished  bool
	}
	_ = json.Unmarshal(b, &m)
	role = m.AgentType[strings.LastIndex(m.AgentType, ":")+1:]
	if role == "" {
		role = "subagent"
	}
	return role, m.Finished
}

// ScanOptions of Scan: one session, or the transcripts changed since a date, else the most recent session.
type ScanOptions struct{ Session, Since, Dir, Slug string }

// Scan reads session transcripts and their subagent transcripts and logs them as observed; it returns the number of lines written.
func Scan(p project.Project, top string, o ScanOptions) (n int, slug string, err error) {
	dir := o.Dir
	if dir == "" {
		dir = TranscriptDir(top)
	}
	if st, e := os.Stat(dir); e != nil || !st.IsDir() {
		return 0, "", errors.New("scan: no transcript directory at " + dir + " (Claude Code only; pass --dir)")
	}
	slug = o.Slug
	if slug == "" {
		slug = CurrentSlug(p)
	}
	file := LedgerFile(p, slug)
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	var list []string
	switch {
	case o.Session != "":
		list = []string{filepath.Join(dir, o.Session+".jsonl")}
	case o.Since != "":
		cut, e := time.ParseInLocation("2006-01-02", o.Since, time.Local)
		if e != nil {
			if cut, e = time.Parse(time.RFC3339, o.Since); e != nil {
				return 0, slug, errors.New("scan: --since wants YYYY-MM-DD")
			}
		}
		all, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		for _, f := range all {
			if st, e := os.Stat(f); e == nil && st.ModTime().After(cut) {
				list = append(list, f)
			}
		}
	default:
		list = newest(dir)
	}
	win := Window(p, slug)
	for _, f := range list {
		s := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		j, e := Extract(f, win)
		if e != nil {
			continue
		}
		if j.Messages > 0 {
			n += observe(file, j, "session-"+s, "orchestrator", s, "snapshot", "scan", false)
		}
		subs, _ := filepath.Glob(filepath.Join(dir, s, "subagents", "agent-*.jsonl"))
		for _, a := range subs {
			aid := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(a), ".jsonl"), "agent-")
			role, fin := agentRole(a)
			kind := "snapshot"
			if fin {
				kind = "final"
			}
			aj, e := Extract(a, "")
			if e != nil || aj.Messages == 0 {
				continue
			}
			// a subagent of an earlier task of the same session belongs to that task's ledger
			if aj.LastTS != nil && win != "" && frac.ReplaceAllString(*aj.LastTS, "") < win {
				continue
			}
			n += observe(file, aj, aid, role, s, kind, "scan", false)
		}
	}
	return n, slug, nil
}

// newest is the most recently modified session transcript of dir (a one-element list, or none).
func newest(dir string) []string {
	all, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	best, bt := "", time.Time{}
	for _, f := range all {
		if st, e := os.Stat(f); e == nil && (best == "" || st.ModTime().After(bt)) {
			best, bt = f, st.ModTime()
		}
	}
	if best == "" {
		return nil
	}
	return []string{best}
}

// Reconcile fills in subagent runs logged as unmeasured (the transcript was not written yet at SubagentStop) from the transcripts now on
// disk; it returns how many ledger lines it wrote.
func Reconcile(p project.Project, top, dir, slug string) int {
	if dir == "" {
		dir = TranscriptDir(top)
	}
	if slug == "" {
		slug = CurrentSlug(p)
	}
	file := LedgerFile(p, slug)
	if st, e := os.Stat(dir); e != nil || !st.IsDir() {
		return 0
	}
	seen := map[[2]string]bool{}
	var todo [][2]string
	for _, l := range ReadLines(file) {
		k := [2]string{l.ID, l.Session}
		if l.TQ == "unavailable" && l.ID != "" && l.Session != "" && !strings.HasPrefix(l.ID, "rr-") && !seen[k] {
			seen[k] = true
			todo = append(todo, k)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i][0]+"\t"+todo[i][1] < todo[j][0]+"\t"+todo[j][1] })
	n := 0
	for _, k := range todo {
		a := filepath.Join(dir, k[1], "subagents", "agent-"+k[0]+".jsonl")
		aj, e := Extract(a, "")
		if e != nil || aj.Messages == 0 {
			continue
		}
		role, _ := agentRole(a)
		n += observe(file, aj, k[0], role, k[1], "final", "reconcile", true)
	}
	return n
}
