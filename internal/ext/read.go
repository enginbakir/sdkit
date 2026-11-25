package ext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/wecii/sdkit/internal/ui"
)

// The read verbs of `sdkit ext`: each prints one JSON document, the same data the panel's pages show, with no server in between.

// HealthDoc is `sdkit ext health`.
type HealthDoc struct {
	Ready bool      `json:"ready"`
	Items []ui.Item `json:"items"`
}

// WriteHealth prints the onboarding checklist of root.
func WriteHealth(ctx context.Context, w io.Writer, root string, getenv func(string) string, host string) error {
	items := ui.Health(ctx, root, getenv, host)
	if items == nil {
		items = []ui.Item{}
	}
	return json.NewEncoder(w).Encode(HealthDoc{Ready: ui.Ready(items), Items: items})
}

// SpecRow is one line of `sdkit ext specs`: the panel's item, plus the folder the spec's files are in when that is not this one.
// A spec worked on in its own worktree has its files, drafts and branch there, which the folder open in the editor does not show.
type SpecRow struct {
	ui.SpecItem
	Folder string `json:"folder,omitempty"`
}

// WriteSpecs prints every spec with its state and flow position.
func WriteSpecs(w io.Writer, root string) error {
	items := ui.ListSpecs(root)
	wts := ui.Worktrees(root)
	rows := make([]SpecRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, SpecRow{SpecItem: it, Folder: wts[it.ID]})
	}
	return json.NewEncoder(w).Encode(rows)
}

// SpecDoc is `sdkit ext spec <id>`: the panel's spec page, plus the folder its files live in (a spec in a worktree is not in the
// main folder), so an editor can open them where they are.
type SpecDoc struct {
	ui.Detail
	Root string `json:"root"`
}

// WriteSpec prints the page of one spec: what was done, what can be done now, its files.
func WriteSpec(w io.Writer, root, id string) error {
	r := ui.RootFor(root, id)
	d, err := ui.GetDetail(r, id)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(SpecDoc{Detail: d, Root: r})
}

// SpecBranch moves a spec's tree to the spec's own branch, which is where its published files are edited (the kit's `spec branch`).
func SpecBranch(w io.Writer, root, id string) error {
	if err := ui.SwitchBranch(ui.RootFor(root, id), id); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// SpecChange opens a change on an approved spec; its name is who is connected and the time, never the caller's text.
func SpecChange(w io.Writer, root, id string, now time.Time) error {
	slug, err := ui.StartChange(ui.RootFor(root, id), id, ui.ReadConnection(root).JiraUser, now)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]string{"slug": slug})
}

// PrespecResult is what saving a pre-spec answers. Exists is true (and nothing was written) when the pre-spec is already there and
// the caller did not ask to replace it; any other refusal is an error with words for the person.
type PrespecResult struct {
	Saved  bool   `json:"saved"`
	Exists bool   `json:"exists,omitempty"`
	ID     string `json:"id"`
}

// WritePrespec stores the text on in as the pre-spec id.
func WritePrespec(w io.Writer, in io.Reader, root, id string, overwrite bool) error {
	text, err := io.ReadAll(io.LimitReader(in, 256<<10))
	if err != nil {
		return err
	}
	err = ui.WritePrespec(root, id, string(text), overwrite)
	switch {
	case errors.Is(err, ui.ErrPrespecExists):
		return json.NewEncoder(w).Encode(PrespecResult{Exists: true, ID: id})
	case err != nil:
		return err
	}
	return json.NewEncoder(w).Encode(PrespecResult{Saved: true, ID: id})
}
