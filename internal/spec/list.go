package spec

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one spec the project knows: a pre-spec, a draft or a spec directory, with where it stands.
type Entry struct {
	ID     string
	State  string // a State* value
	Human  bool   // the next step is a person's (a decision, or the Tech Lead's merge)
	Action string
}

// List names every id that has a pre-spec file, a draft or a spec directory, sorted, each with its state from Next. Nothing is
// written. An id the project would refuse as a name is left out.
func List(root string) []Entry {
	e := envAt(root, nil, nil)
	ids := map[string]bool{}
	add := func(dir string, files bool) {
		ents, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		for _, d := range ents {
			name := d.Name()
			switch {
			case files && !d.IsDir() && strings.HasSuffix(name, ".md"):
				name = strings.TrimSuffix(name, ".md")
			case !files && d.IsDir():
			default:
				continue
			}
			if okname(name) && !strings.HasPrefix(name, ".") && strings.ToUpper(name) != "README" {
				ids[name] = true
			}
		}
	}
	add(e.prespec, true)
	add(e.specs, false)
	add(e.kit+"/work/spec-draft", false)
	out := make([]Entry, 0, len(ids))
	for id := range ids {
		s := Next(root, id)
		// Once a spec has gone to a PR, where it stands is in its own record. Next would look for review evidence in this machine's
		// work directory, which a fresh clone (or another analyst's spec) does not have, and call a merged spec "in review".
		if e.hasMeta(id) {
			if m := e.meta(id); len(m.History) > 0 {
				s.State, s.Human, s.Action = StateAwaiting, true, "wait for the Tech Lead's merge"
				if m.Status == "approved" {
					s.State, s.Human, s.Action = StateApproved, false, "done"
				}
			}
		}
		out = append(out, Entry{ID: id, State: s.State, Human: s.Human, Action: s.Action})
	}
	// what the analyst can still move first, then what waits for others, then what is done; by id inside each group
	rank := map[string]int{StateAwaiting: 1, StateApproved: 2}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank[out[i].State], rank[out[j].State]; ri != rj {
			return ri < rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}
