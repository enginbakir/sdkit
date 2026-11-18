package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/spec"
)

// SpecItem is one row of the home screen.
type SpecItem struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Label    string `json:"label"`
	Waiting  bool   `json:"waiting"`  // someone other than the agent must act (the analyst's answer, the Tech Lead's merge)
	Continue bool   `json:"continue"` // the agent can take it from here
}

// stateText is the plain-words state; whether the agent can continue it follows from the state.
var stateText = map[string]struct {
	label string
	cont  bool
}{
	spec.StateNoInput:     {"Ön-spec yok", false},
	spec.StateNew:         {"Ön-spec hazır, spec üretimi başlamadı", true},
	spec.StateDraft:       {"Taslak hazırlanıyor", true},
	spec.StateOutput:      {"Spec gözden geçiriliyor", true},
	spec.StateUnpublished: {"Gözden geçirildi, yayına hazır", true},
	spec.StateAwaiting:    {"Tech Lead onayı bekliyor", false},
	spec.StateApproved:    {"Onaylandı", false},
}

// ListSpecs is every spec of the project with where it stands, from the same facts `sdkit spec next` reads.
func ListSpecs(root string) []SpecItem {
	var out []SpecItem
	for _, e := range spec.List(root) {
		t, ok := stateText[e.State]
		if !ok {
			t.label = e.State
		}
		out = append(out, SpecItem{ID: e.ID, State: e.State, Label: t.label, Continue: t.cont, Waiting: e.State == spec.StateAwaiting})
	}
	return out
}

// reID is stricter than the kit's own name rule: it becomes a file name, a branch name and part of a Jira link, and a person typed it.
var reID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,59}$`)

// ValidID reports whether id may name a new pre-spec from the panel.
func ValidID(id string) bool { return reID.MatchString(id) }

const maxPrespec = 200 << 10

// WritePrespec saves the person's text as the pre-spec <id> in the project's pre-spec directory. The pre-spec is working input and
// is never committed, so it refuses when git would not ignore the file. An existing pre-spec is only replaced on request.
func WritePrespec(root, id, text string, overwrite bool) error {
	switch {
	case !ValidID(id):
		return errors.New("Ad harf, rakam, tire ve alt çizgiden oluşmalı (en fazla 60 karakter), örneğin odeme-akisi")
	case strings.TrimSpace(text) == "":
		return errors.New("Ön-spec boş olamaz")
	case len(text) > maxPrespec:
		return errors.New("Ön-spec çok uzun")
	}
	s, _, _ := project.Load(root)
	dir := strings.Trim(s.String("PRESPEC_DIR"), "/")
	if dir == "" || strings.HasPrefix(dir, "__") {
		dir = "prespec"
	}
	if filepath.IsAbs(dir) || strings.Contains(dir, "..") {
		return errors.New("PRESPEC_DIR bu deponun içinde olmalı")
	}
	path := filepath.Join(root, filepath.FromSlash(dir), id+".md")
	if gitx.Tracked(root, path) || !gitx.IsIgnored(root, path) {
		return fmt.Errorf("%s git tarafından yok sayılmıyor; ön-spec yanlışlıkla paylaşılmasın diye kaydedilmedi", dir)
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return fmt.Errorf("%q adında bir ön-spec zaten var", id)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// Task is what a run is told to do. The browser names an action and an id; the wording is ours, so a page cannot hand the agent
// arbitrary instructions.
func Task(action, id string) (string, error) {
	if !ValidID(id) && !reKitID.MatchString(id) {
		return "", errors.New("geçersiz ad")
	}
	switch action {
	case "generate-specs":
		return fmt.Sprintf("Use the generate-specs skill for the spec id %q (the pre-spec has the same name). If work on it already exists, continue from where `sdkit spec next %s` says it stands.", id, id), nil
	}
	return "", errors.New("bilinmeyen işlem")
}

// reKitID admits the ids that already exist in a project (they may carry a dot), for continuing them.
var reKitID = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,79}$`)
