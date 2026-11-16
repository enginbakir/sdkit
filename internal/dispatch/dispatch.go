// Package dispatch is the ledger of parallel tasks and the rule that decides whether a task may start now. It starts nothing.
// The ledger is <project-dir>/work/<slug>/dispatch.jsonl, one event per line, serialised with store.Update.
package dispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/store"
)

// Refused is a rule that says no; the message names the reason.
type Refused string

func (r Refused) Error() string { return string(r) }

type event struct {
	TS    string `json:"ts"`
	Event string `json:"event"`
	Task  string `json:"task"`
	Write string `json:"writes,omitempty"`
	Head  string `json:"head"`
}

// Entry is the state of one task: "active", "done" or "free" (released).
type Entry struct{ Task, State, Writes string }

// Ledger is one task's dispatch.jsonl. Max < 1 means 3. Head returns the short HEAD (may be nil).
type Ledger struct {
	Path string
	Max  int
	Head func() string
}

func (l Ledger) max() int {
	if l.Max < 1 {
		return 3
	}
	return l.Max
}

// States folds the events into one Entry per task, in first-seen order.
func States(data []byte) []Entry {
	var out []Entry
	idx := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Task == "" {
			continue
		}
		i, ok := idx[e.Task]
		if !ok {
			i = len(out)
			idx[e.Task] = i
			out = append(out, Entry{Task: e.Task})
		}
		switch e.Event {
		case "claim":
			out[i].State, out[i].Writes = "active", e.Write
		case "done":
			out[i].State = "done"
		default:
			out[i].State = "free"
		}
	}
	return out
}

func stateOf(es []Entry, id string) string {
	for _, e := range es {
		if e.Task == id {
			return e.State
		}
	}
	return "free"
}

func (l Ledger) line(ev, id, writes string) []byte {
	h := ""
	if l.Head != nil {
		h = l.Head()
	}
	b, _ := json.Marshal(event{time.Now().UTC().Format("2006-01-02T15:04:05Z"), ev, id, writes, h})
	return append(b, '\n')
}

// overlap: write sets conflict when a path equals or contains one of the other (the rule of plan waves).
func overlap(a, b string) bool {
	for _, p := range strings.Split(a, ",") {
		for _, q := range strings.Split(b, ",") {
			p, q = strings.TrimRight(strings.TrimSpace(p), "/"), strings.TrimRight(strings.TrimSpace(q), "/")
			if p != "" && q != "" && (strings.HasPrefix(p+"/", q+"/") || strings.HasPrefix(q+"/", p+"/")) {
				return true
			}
		}
	}
	return false
}

// Claim takes task id from tasksText. Claiming an active or done task changes nothing and is refused.
func (l Ledger) Claim(id, tasksText string) (string, error) {
	ts, err := plan.ParseTasks(tasksText)
	var t *plan.Task
	for i := range ts {
		if ts[i].ID == id {
			t = &ts[i]
		}
	}
	if t == nil {
		if err != nil {
			return "", err
		}
		return "", Refused("task " + id + " is not in the tasks file")
	}
	msg := ""
	err = store.Update(l.Path, func(old []byte) ([]byte, error) {
		es := States(old)
		switch stateOf(es, id) {
		case "active":
			return nil, Refused(id + " is already dispatched and not finished (no duplicate work)")
		case "done":
			return nil, Refused(id + " is already done and merged")
		}
		for _, a := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(t.After, " ", ""), ",", " "), " ") {
			if a != "" && stateOf(es, a) != "done" {
				return nil, Refused(id + " waits for " + a + " (after:), which is not done")
			}
		}
		n := 0
		for _, e := range es {
			if e.State == "active" {
				n++
			}
		}
		if n >= l.max() {
			return nil, Refused(fmt.Sprintf("%d task(s) already active, MAX_PARALLEL=%d", n, l.max()))
		}
		for _, e := range es {
			if e.State == "active" && overlap(t.Writes, e.Writes) {
				return nil, Refused(fmt.Sprintf("%s writes %s, which overlaps %s of active task %s", id, t.Writes, e.Writes, e.Task))
			}
		}
		msg = fmt.Sprintf("claimed %s (active %d/%d)", id, n+1, l.max())
		return append(old, l.line("claim", id, strings.ReplaceAll(t.Writes, " ", ""))...), nil
	})
	return msg, err
}

// Finish records "done" (after the merge) or "release" (interrupted) for an active task.
func (l Ledger) Finish(id string, release bool) (string, error) {
	ev, msg := "done", "done "+id+". The merged diff changed: every recorded verifier / reviewer / security-auditor PASS is stale until it is run again on the merged diff (sdkit sdd evidence)."
	if release {
		ev, msg = "release", "released "+id
	}
	err := store.Update(l.Path, func(old []byte) ([]byte, error) {
		if s := stateOf(States(old), id); s != "active" {
			return nil, Refused(id + " is not active (" + s + ")")
		}
		return append(old, l.line(ev, id, "")...), nil
	})
	return msg, err
}

// Status prints the limit, the counts and one line per task.
func (l Ledger) Status(data []byte) []string {
	es := States(data)
	a, d := 0, 0
	for _, e := range es {
		if e.State == "active" {
			a++
		} else if e.State == "done" {
			d++
		}
	}
	out := []string{fmt.Sprintf("parallel limit %d, active %d, done %d", l.max(), a, d)}
	for _, e := range es {
		w := ""
		if e.State == "active" {
			w = " writes=" + e.Writes
		}
		out = append(out, "  "+e.Task+" "+e.State+w)
	}
	return out
}
