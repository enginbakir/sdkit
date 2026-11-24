package design

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Meta ties a design to the exact spec it was made from, so a later spec change is noticed.
type Meta struct {
	Spec        string `json:"spec"`
	SpecVersion string `json:"specVersion"`
	SpecHash    string `json:"specHash"`
}

// File is one output of a build.
type File struct {
	Name string
	Data []byte
}

// Render returns the files a design directory holds: the model as written, the BPMN, its Markdown twin and the meta.
func Render(m *Model, raw []byte, sp Spec) []File {
	meta, _ := json.MarshalIndent(Meta{m.Spec, sp.Version, sp.Hash}, "", "  ")
	return []File{
		{"process.json", raw},
		{"process.bpmn", BPMN(m)},
		{"process.md", Markdown(m, sp.Version)},
		{"design.meta.json", append(meta, '\n')},
	}
}

// Write puts the rendered files into dir.
func Write(dir string, fs []File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range fs {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Verify checks a built design directory: the model against the spec, the outputs against a fresh render (nobody edited them by
// hand, nobody forgot to rebuild) and the spec it was made from against the spec now.
func Verify(dir string, sp Spec) []Problem {
	m, raw, err := Load(filepath.Join(dir, "process.json"))
	if err != nil {
		return []Problem{{"error", err.Error()}}
	}
	ps := Check(m, sp)
	if Errors(ps) > 0 {
		return ps
	}
	for _, f := range Render(m, raw, sp) {
		if f.Name == "design.meta.json" {
			continue // its spec fields are judged below, not compared
		}
		b, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil || !bytes.Equal(b, f.Data) {
			ps = append(ps, Problem{"error", fmt.Sprintf("%s is missing or differs from process.json: run sdkit design build %s", f.Name, m.Spec)})
		}
	}
	var meta Meta
	b, err := os.ReadFile(filepath.Join(dir, "design.meta.json"))
	if err != nil || json.Unmarshal(b, &meta) != nil {
		return append(ps, Problem{"error", "design.meta.json is missing or unreadable: run sdkit design build " + m.Spec})
	}
	if meta.SpecHash != sp.Hash {
		ps = append(ps, Problem{"warn", fmt.Sprintf("STALE: the design was built from spec v%s and the spec is now v%s with other content; re-check the process against the spec, then run sdkit design build %s", meta.SpecVersion, sp.Version, m.Spec)})
	}
	return ps
}
