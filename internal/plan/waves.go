package plan

import (
	"fmt"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Task is one tasks.md line.
type Task struct {
	ID, Role, Size, Writes, After string
}

// ParseTasks reads the task lines (`- [ ] T3 text | role: | size: | writes: a/,b.go | after: T1`).
// It fails on a duplicate id (after reading every line, as the bash did).
func ParseTasks(text string) ([]Task, error) {
	var ts []Task
	seen := map[string]bool{}
	var dups []string
	for _, l := range strings.Split(text, "\n") {
		if !reTaskRow.MatchString(l) {
			continue
		}
		f := strings.Split(reTaskPre.ReplaceAllString(l, ""), "|")
		t := Task{}
		if h := strings.Fields(f[0]); len(h) > 0 {
			t.ID = h[0]
		}
		if seen[t.ID] {
			dups = append(dups, "duplicate task id "+t.ID)
			continue
		}
		seen[t.ID] = true
		for _, kv := range f[1:] {
			kv = strings.TrimSpace(kv)
			switch {
			case strings.HasPrefix(kv, "writes:"):
				t.Writes = strings.TrimLeft(kv[7:], " ")
			case strings.HasPrefix(kv, "after:"):
				t.After = strings.TrimLeft(kv[6:], " ")
			case strings.HasPrefix(kv, "role:"):
				t.Role = strings.TrimLeft(kv[5:], " ")
			case strings.HasPrefix(kv, "size:"):
				t.Size = strings.TrimLeft(kv[5:], " ")
			}
		}
		ts = append(ts, t)
	}
	if len(dups) > 0 {
		return ts, fmt.Errorf("%s", strings.Join(dups, "\n"))
	}
	return ts, nil
}

func paths(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimRight(strings.TrimSpace(p), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// conflict: two tasks conflict when a write path equals or contains the other's.
func conflict(a, b Task) bool {
	for _, pa := range paths(a.Writes) {
		for _, pb := range paths(b.Writes) {
			if pa == pb || strings.HasPrefix(pa+"/", pb+"/") || strings.HasPrefix(pb+"/", pa+"/") {
				return true
			}
		}
	}
	return false
}

// Waves groups tasks into parallel waves (at most max per wave; max < 1 means 3). A wave line is
// "wave N: T1(role,size)  ||  T2". Errors: unknown dependency, cycle.
func Waves(ts []Task, max int) ([]string, error) {
	if max < 1 {
		max = 3
	}
	known, done := map[string]bool{}, map[string]bool{}
	for _, t := range ts {
		known[t.ID] = true
	}
	deps := map[string][]string{}
	for _, t := range ts {
		for _, d := range strings.Split(t.After, ",") {
			if d = strings.TrimSpace(d); d != "" {
				if !known[d] {
					return nil, fmt.Errorf("%s depends on unknown task %s", t.ID, d)
				}
				deps[t.ID] = append(deps[t.ID], d)
			}
		}
	}
	var out []string
	for left := len(ts); left > 0; {
		var pick []Task
	next:
		for _, t := range ts {
			if done[t.ID] || len(pick) >= max {
				continue
			}
			for _, d := range deps[t.ID] {
				if !done[d] {
					continue next
				}
			}
			for _, p := range pick {
				if conflict(t, p) {
					continue next
				}
			}
			pick = append(pick, t)
		}
		if len(pick) == 0 {
			return out, fmt.Errorf("dependency cycle among remaining tasks")
		}
		var parts []string
		for _, t := range pick {
			done[t.ID] = true
			left--
			l := t.Role
			if t.Size != "" {
				if t.Role != "" {
					l += ","
				}
				l += t.Size
			}
			if l != "" {
				l = "(" + l + ")"
			}
			parts = append(parts, t.ID+l)
		}
		out = append(out, fmt.Sprintf("wave %d: %s", len(out)+1, strings.Join(parts, "  ||  ")))
	}
	return out, nil
}

// CheckWrites returns the changed files that fall outside the writes set of task id, the writes
// string (spaces removed) and whether the task exists. A task without writes is read-only.
func CheckWrites(tasksText, id string, changed []string) (bad []string, writes string, found bool) {
	ts, _ := ParseTasks(tasksText)
	var t Task
	for _, x := range ts {
		if x.ID == id {
			t, found = x, true
			break
		}
	}
	if !found {
		return nil, "", false
	}
	writes = strings.ReplaceAll(t.Writes, " ", "")
	for _, f := range changed {
		in := false
		for _, p := range paths(writes) {
			if f == p || strings.HasPrefix(f, p+"/") {
				in = true
			}
		}
		if f != "" && !in {
			bad = append(bad, f)
		}
	}
	return bad, writes, true
}
