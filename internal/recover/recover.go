// Package recover finds interrupted or stale bookkeeping and closes it (what scripts/recover.sh did). It never dispatches, retries, re-runs
// a role, edits evidence or turns a BLOCK into anything else. "Stale" is older than RECOVER_STALE_S (default 1800 s).
package recover

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/store"
)

// Item is one thing a crash left open and the single safe action for it.
type Item struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	Applicable bool   `json:"applicable"`
}

// Scanner reads one project and one task. Reservations is the seam of the budget reservations (P4): nil finds none.
type Scanner struct {
	P            project.Project
	Slug         string
	TTL          time.Duration
	Now          time.Time
	Reservations func() []Item
}

func (s Scanner) work() string { return filepath.Join(s.P.Dir(), "work", s.Slug) }

func (s Scanner) age(t time.Time) time.Duration { return s.Now.Sub(t) }

func (s Scanner) iso(v string) time.Time {
	if t, err := time.Parse("2006-01-02T15:04:05Z", v); err == nil {
		return t
	}
	return s.Now
}

func secs(d time.Duration) int64 { return int64(d / time.Second) }

func jsonLines(path string, fn func(m map[string]any)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			fn(m)
		}
	}
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// Scan lists the stale items; it changes nothing.
func (s Scanner) Scan() []Item {
	items := []Item{}
	add := func(kind, id, action, detail string, applicable bool) {
		items = append(items, Item{kind, id, action, detail, applicable})
	}
	// 1. contract handoffs issued and never answered
	cs, _ := filepath.Glob(filepath.Join(s.work(), "contracts", "*.json"))
	for _, f := range cs {
		b, err := os.ReadFile(f)
		var c struct{ State string }
		st, err2 := os.Stat(f)
		if err != nil || err2 != nil || json.Unmarshal(b, &c) != nil || c.State != "issued" {
			continue
		}
		if a := s.age(st.ModTime()); a >= s.TTL {
			id := s.Slug + "/" + strings.TrimSuffix(filepath.Base(f), ".json")
			add("contract", id, "sdkit contract close "+id+" --reason interrupted", fmt.Sprintf("issued %ds ago, no result", secs(a)), true)
		}
	}
	// 2. subagent registrations that never closed; evidence that no longer matches
	stPath := filepath.Join(s.P.Dir(), ".sdd", "state.json")
	if b, err := os.ReadFile(stPath); err == nil {
		t, err := sdd.ParseTask(b)
		if err != nil {
			add("state", stPath, "restore state.json from your own copy; nothing is guessed", "unreadable JSON", false)
		} else {
			for _, sp := range t.Spawns {
				if sp.At == "" {
					continue
				}
				if a := s.age(s.iso(sp.At)); a >= s.TTL {
					id, flags := sp.Run, "--run "+sp.Run
					if id == "" {
						id, flags = sp.Role, "--role "+sp.Role+" --source hook"
					}
					add("spawn", id, "sdkit sdd _spawn-close "+flags+" --reason interrupted", fmt.Sprintf("%s registered %ds ago, no answer recorded", sp.Role, secs(a)), true)
				}
			}
			fp, _ := sdd.New(s.P.Root).Fingerprint()
			roles := make([]string, 0, len(t.Evidence))
			for r := range t.Evidence {
				roles = append(roles, r)
			}
			sort.Strings(roles)
			for _, r := range sdd.EvidenceTable(t, fp, roles) {
				if r.State != "valid" && r.State != "missing" {
					add("evidence", r.Role, "run the "+r.Role+" again on the current diff (its recorded result no longer matches)", "state: "+r.State, false)
				}
			}
		}
	}
	// 3. dispatch claims never finished (the last event of a task is a claim)
	last := map[string]map[string]any{}
	jsonLines(filepath.Join(s.work(), "dispatch.jsonl"), func(m map[string]any) { last[str(m, "task")] = m })
	tasks := make([]string, 0, len(last))
	for t := range last {
		tasks = append(tasks, t)
	}
	sort.Strings(tasks)
	for _, t := range tasks {
		if m := last[t]; str(m, "event") == "claim" {
			if a := s.age(s.iso(str(m, "ts"))); a >= s.TTL {
				add("dispatch", t, "sdkit dispatch release "+t+" --slug "+s.Slug, fmt.Sprintf("claimed %ds ago, not done", secs(a)), true)
			}
		}
	}
	// 4. routing decisions in flight whose process is gone (reported; the router closes them itself)
	done := map[string]bool{}
	var open []map[string]any
	jsonLines(filepath.Join(s.work(), "routing.jsonl"), func(m map[string]any) {
		switch a := str(m, "action"); {
		case str(m, "type") == "outcome":
			done[str(m, "id")] = true
		case str(m, "type") == "decision" && (a == "execute" || a == "retry" || a == "escalate"):
			open = append(open, m)
		}
	})
	for _, m := range open {
		id := str(m, "id")
		if id == "" || done[id] {
			continue
		}
		if pid, ok := m["pid"].(float64); ok && pid > 0 && proc.Alive(int(pid)) {
			continue
		}
		add("routing", id, "none: the next route decide closes it as INTERRUPTED and counts it as one attempt", "no outcome and no live process", false)
	}
	// 5. budget reservations whose run is gone
	if s.Reservations != nil {
		items = append(items, s.Reservations()...)
	}
	return items
}

// Apply closes every applicable item through closer (item -> exit code) and logs each to work/<slug>/recovery.jsonl.
// It returns the number closed and the number left for a person.
func (s Scanner) Apply(items []Item, closer func(Item) int) (closed, manual int) {
	log := filepath.Join(s.work(), "recovery.jsonl")
	_ = os.MkdirAll(s.work(), 0o755)
	for _, it := range items {
		if !it.Applicable {
			manual++
			continue
		}
		rc := closer(it)
		line, _ := json.Marshal(struct {
			TS   string `json:"ts"`
			Kind string `json:"kind"`
			ID   string `json:"id"`
			RC   int    `json:"rc"`
			Note string `json:"note"`
		}{time.Now().UTC().Format("2006-01-02T15:04:05Z"), it.Kind, it.ID, rc, "bookkeeping closed; nothing was re-run"})
		_ = store.AppendLine(log, string(line))
		closed++
	}
	return closed, manual
}
