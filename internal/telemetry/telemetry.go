// Package telemetry is the local event log: one JSON line per event in <project-dir>/work/<task>/events.jsonl, off unless TELEMETRY=1.
// It never changes a decision: a failure drops the event. Schema and limits: docs/design-telemetry.md.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/store"
)

var (
	types   = []string{"task.start", "task.end", "phase.enter", "phase.reject", "agent.start", "agent.end", "retry", "test.result", "verify.result", "failure", "bench.mark", "contract.check", "workflow.decision"}
	cats    = []string{"budget", "gate", "tests", "verdict-block", "verdict-malformed", "host-error", "host-missing", "timeout", "contract", "other"}
	reasons = []string{"no-result", "malformed", "unsupported-version", "missing-field", "bad-value", "inconsistent", "id-mismatch", "role-mismatch", "task-mismatch", "unknown-id", "duplicate", "stale", "unsafe-path", "missing-artifact", "out-of-scope", "not-next", "unknown-phase", "no-artifact", "no-dod", "no-design", "tests-pass", "tests-fail", "verify-failed", "no-verdict", "verdict-block", "stale-verdict", "risk-floor", "profile-risk", "agents-pending", "retry-exhausted", "out-of-phase", "concurrent", "no-evidence", "evidence-block", "stale-evidence", "untrusted-evidence", "role-conflict", "no-task-size"}
	ints    = []string{"dur_ms", "in", "out", "cache_read", "cache_write", "tool_calls", "attempt", "bytes", "cv"}
	strs    = map[string]bool{"span": true, "parent": true, "phase": true, "from": true, "to": true, "role": true, "host": true, "model": true, "tier": true, "outcome": true, "category": true, "source": true, "risk": true, "contract": true, "stage": true, "reason": true, "status": true, "profile": true}
	slugRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	tsRe    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
)

// NowMs is the clock in milliseconds since the epoch.
func NowMs() int64 { return time.Now().UnixMilli() }

// Enabled reports whether TELEMETRY=1 for the project at root.
func Enabled(root string) bool {
	s, _, _ := project.Load(root)
	return base.Getter(s.Raw)("TELEMETRY") == "1"
}

// Emit appends one event for the open task. fields alternate key and value; a key is a flag name without "--"
// (task, run, note, dur-ms, in, out, q "metric=quality" ...). Off unless TELEMETRY=1 or force; strict returns the reason an event
// is invalid instead of dropping it silently.
func Emit(root, typ string, fields []string, force, strict bool) error {
	if typ == "" || (!force && !Enabled(root)) {
		return nil
	}
	p := project.Resolve(root)
	task, run, note := "", "", ""
	type kv struct {
		k string
		v any
	}
	var ev []kv
	q := map[string]any{}
	qOrder := []string{}
	for i := 0; i < len(fields); i += 2 {
		k, v := fields[i], ""
		if i+1 < len(fields) {
			v = fields[i+1]
		}
		switch {
		case k == "task":
			task = v
		case k == "run":
			run = v
		case k == "note":
			note = strings.Join(strings.Fields(strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(v)), " ")
			note = clip(note, 160)
		case k == "q":
			m, val, _ := strings.Cut(v, "=")
			if val != "" {
				if _, ok := q[m]; !ok {
					qOrder = append(qOrder, m)
				}
				q[m] = val
			}
		case strs[k]:
			if v != "" {
				ev = append(ev, kv{k, clip(v, 120)})
			}
		case slices.Contains(ints, strings.ReplaceAll(k, "-", "_")):
			n, err := strconv.ParseUint(v, 10, 53)
			if err != nil {
				if strict {
					return fmt.Errorf("%s is not a non-negative integer", k)
				}
				continue
			}
			ev = append(ev, kv{strings.ReplaceAll(k, "-", "_"), n})
		}
	}
	if task == "" {
		if t := state.ReadOpen(p).Task; state.ValidSlug(t) {
			task = t
		} else {
			task = state.ActiveSlug(p)
		}
	}
	if task == "" {
		task = "_misc"
	}
	if !slugRe.MatchString(task) {
		return errors.New("bad task slug")
	}
	dir := filepath.Join(p.Dir(), "work", task)
	if run == "" {
		run = runID(dir, typ == "task.start")
	}
	mode := os.Getenv("SDKIT_TELEMETRY_MODE")
	if mode == "" {
		mode = "live"
	}
	m := map[string]any{"v": 1, "ts": state.Now(), "type": typ, "task": task, "run": run, "mode": mode}
	if note != "" {
		m["note"] = note
	}
	for _, e := range ev {
		m[e.k] = e.v
	}
	has := func(k string) bool { _, ok := m[k]; return ok }
	if typ == "agent.end" {
		if !has("in") && !has("out") {
			set(q, &qOrder, "tokens", "unavailable")
		}
		if !has("dur_ms") {
			set(q, &qOrder, "duration", "unavailable")
		}
	}
	if len(qOrder) > 0 {
		m["q"] = q
	}
	if errs := Errors(m); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	// fixed key order: the base fields, the note, then the rest as given; q last
	b := []byte("{")
	add := func(k string, v any) {
		j, _ := json.Marshal(v)
		if len(b) > 1 {
			b = append(b, ',')
		}
		b = append(b, fmt.Sprintf("%q:", k)...)
		b = append(b, j...)
	}
	for _, k := range []string{"v", "ts", "type", "task", "run", "mode", "note"} {
		if has(k) {
			add(k, m[k])
		}
	}
	for _, e := range ev {
		add(e.k, e.v)
	}
	if len(qOrder) > 0 {
		var qb strings.Builder
		for i, k := range qOrder {
			if i > 0 {
				qb.WriteByte(',')
			}
			j, _ := json.Marshal(q[k])
			fmt.Fprintf(&qb, "%q:%s", k, j)
		}
		add("q", json.RawMessage("{"+qb.String()+"}"))
	}
	b = append(b, '}')
	return store.AppendLine(filepath.Join(dir, "events.jsonl"), string(b))
}

func set(q map[string]any, order *[]string, k, v string) {
	if _, ok := q[k]; !ok {
		*order = append(*order, k)
	}
	q[k] = v
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// RunID is the current run of a task: SDKIT_RUN_ID, else work/<task>/run.id (made on first use).
func RunID(root, task string) string {
	return runID(filepath.Join(project.Resolve(root).Dir(), "work", task), false)
}

func runID(dir string, fresh bool) string {
	if id := os.Getenv("SDKIT_RUN_ID"); id != "" {
		return id
	}
	f := filepath.Join(dir, "run.id")
	if !fresh {
		if b, err := os.ReadFile(f); err == nil && len(b) > 0 {
			return string(b)
		}
	}
	id := fmt.Sprintf("r%s-%d%d", time.Now().UTC().Format("20060102150405"), os.Getpid(), time.Now().Nanosecond()%32768)
	if unlock, err := store.Lock(f); err == nil {
		defer unlock()
		if b, err := os.ReadFile(f); !fresh && err == nil && len(b) > 0 {
			return string(b)
		}
		_ = store.WriteAtomic(f, []byte(id))
	}
	return id
}

// Errors lists what is wrong with an event (empty: valid). The same rules check a line to emit and a stored line.
func Errors(m map[string]any) []string {
	var e []string
	str := func(k string) (string, bool) { s, ok := m[k].(string); return s, ok }
	in := func(k string, list []string) bool { s, _ := str(k); return slices.Contains(list, s) }
	need := func(k string) {
		if s, _ := str(k); s == "" {
			if _, isNum := m[k].(float64); !isNum {
				if _, isU := m[k].(uint64); !isU {
					e = append(e, fmt.Sprintf("%v needs %s", m["type"], k))
				}
			}
		}
	}
	if n, ok := m["v"].(int); !(ok && n == 1) {
		if f, ok := m["v"].(float64); !ok || f != 1 {
			e = append(e, "v must be 1")
		}
	}
	if ts, ok := str("ts"); !ok || !tsRe.MatchString(ts) {
		e = append(e, "ts must be YYYY-MM-DDTHH:MM:SSZ")
	}
	if !in("type", types) {
		e = append(e, "unknown type")
	}
	if t, _ := str("task"); !slugRe.MatchString(t) {
		e = append(e, "task must be a slug")
	}
	if r, _ := str("run"); r == "" {
		e = append(e, "run is required")
	}
	if !in("mode", []string{"live", "simulated"}) {
		e = append(e, "mode must be live or simulated")
	}
	if qm, ok := m["q"].(map[string]any); ok {
		for k, v := range qm {
			if s, _ := v.(string); !slices.Contains([]string{"actual", "estimated", "unavailable"}, s) {
				e = append(e, "q."+k+" must be actual, estimated or unavailable")
			}
		}
	}
	if _, ok := m["category"]; ok && !in("category", cats) {
		e = append(e, "unknown category")
	}
	if _, ok := m["reason"]; ok && !in("reason", reasons) {
		e = append(e, "unknown reason")
	}
	if _, ok := m["status"]; ok && !in("status", []string{"done", "partial", "blocked", "failed"}) {
		e = append(e, "status must be done, partial, blocked or failed")
	}
	if _, ok := m["profile"]; ok && !in("profile", []string{"small", "standard", "high-assurance"}) {
		e = append(e, "unknown profile")
	}
	switch m["type"] {
	case "agent.start", "agent.end":
		need("role")
	case "phase.enter":
		need("to")
	case "task.end", "verify.result", "test.result":
		need("outcome")
	case "failure":
		need("category")
	case "workflow.decision":
		need("source")
		need("outcome")
	case "contract.check":
		need("outcome")
		need("stage")
		need("cv")
		if !in("outcome", []string{"valid", "invalid"}) {
			e = append(e, "contract.check outcome must be valid or invalid")
		}
		if !in("stage", []string{"handoff", "result"}) {
			e = append(e, "stage must be handoff or result")
		}
		if o, _ := str("outcome"); o == "invalid" {
			need("reason")
		}
	}
	return e
}

// ValidateLine checks one stored line against the schema.
func ValidateLine(line []byte) []string {
	var m map[string]any
	if json.Unmarshal(line, &m) != nil || m == nil {
		return []string{"not an object"}
	}
	for _, k := range ints {
		if f, ok := m[k]; ok {
			if n, isNum := f.(float64); !isNum || n < 0 || n != float64(int64(n)) {
				return append(Errors(m), k+" must be a non-negative integer")
			}
		}
	}
	return Errors(m)
}
