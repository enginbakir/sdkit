package ext

import (
	"context"
	"encoding/json"
	"errors"
	"io"

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

// WriteSpecs prints every spec with its state and flow position.
func WriteSpecs(w io.Writer, root string) error {
	items := ui.ListSpecs(root)
	if items == nil {
		items = []ui.SpecItem{}
	}
	return json.NewEncoder(w).Encode(items)
}

// WriteSpec prints the page of one spec: what was done, what can be done now, its files.
func WriteSpec(w io.Writer, root, id string) error {
	d, err := ui.GetDetail(ui.RootFor(root, id), id)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(d)
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
