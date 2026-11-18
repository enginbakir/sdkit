package spec

import (
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Rev is one published version of a spec.
type Rev struct {
	Version string `json:"version"`
	At      string `json:"at"`
}

// ChangeInfo is one change of an approved spec.
type ChangeInfo struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"` // open | applied
	Label  string `json:"label"`
}

// Info is what a screen shows about one spec, from its own records. Dir and Draft are relative to the repo, with slashes.
type Info struct {
	Dir     string       `json:"dir"`
	Draft   string       `json:"draft"`
	Version string       `json:"version"`
	Status  string       `json:"status"`
	Verdict string       `json:"verdict"`
	History []Rev        `json:"history"`
	Changes []ChangeInfo `json:"changes"`
}

// Inspect reads the spec's meta and its changes. Nothing is written; a spec with no meta (not yet published) gives empty fields.
func Inspect(root, id string) Info {
	e := envAt(root, nil, nil)
	in := Info{Dir: e.dir(id), Draft: e.kit + "/work/spec-draft/" + id, History: []Rev{}, Changes: []ChangeInfo{}}
	m := e.meta(id)
	in.Version, in.Status, in.Verdict = m.Version, m.Status, m.Review.Verdict
	for _, h := range m.History {
		in.History = append(in.History, Rev{h.Version, h.At})
	}
	ents, _ := os.ReadDir(filepath.Join(e.abs(id), "changes"))
	for _, c := range ents {
		cj := filepath.Join(e.cabs(id, c.Name()), "change.json")
		if _, err := os.Stat(cj); err != nil {
			continue
		}
		in.Changes = append(in.Changes, ChangeInfo{Slug: c.Name(), Title: fileStr(cj, "title"), Status: fileStr(cj, "status"), Label: fileStr(cj, "label")})
	}
	sort.Slice(in.Changes, func(i, j int) bool { return in.Changes[i].Slug < in.Changes[j].Slug })
	return in
}

// RunSpecAt is `sdkit spec <verb> ...` on the project at root instead of the one the process is in: the panel serves one repo
// whatever its working directory is.
func RunSpecAt(root string, args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		return RunSpec(args, out, errw)
	}
	return envAt(root, out, errw).dispatch(args[0], args[1:])
}
