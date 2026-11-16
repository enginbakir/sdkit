package review

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStatus(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "s1.md") // the document doubles as the empty findings array
	os.WriteFile(doc, []byte("[]"), 0o644)
	rec := func(v, iso string) string {
		Load(root).Begin("s1", doc, false)
		Load(root).Record("s1", v, iso, doc)
		return Status(root, "s1").State
	}
	got := fmt.Sprintln(Status(root, "s1").State, rec("NOT_RUN", "none"), rec("PASS", "none"), rec("PASS", "fresh"))
	os.WriteFile(doc, []byte("b"), 0o644)
	if got += Status(root, "s1").State; got != "NOT_RUN NOT_RUN NOT_RUN PASS\nstale" {
		t.Fatal(got)
	}
}

// With REVIEW_REQUIRE_LINT a mechanical error stops begin before a round is used.
func TestBeginRequiresLint(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "spec.md")
	os.WriteFile(doc, []byte("#### Scenario: AC-X-1\n#### Scenario: AC-X-1\n"), 0o644)
	c := Load(root)
	c.RequireLint = true
	if _, err := c.Begin("s", doc, false); err == nil {
		t.Fatal("duplicate AC id must stop begin")
	}
	if st, _ := readState(c.stateFile("s")); st.Round != 0 {
		t.Fatalf("round used: %d", st.Round)
	}
	os.WriteFile(doc, []byte("#### Scenario: AC-X-1\n#### Scenario: AC-X-2\n"), 0o644)
	if _, err := c.Begin("s", doc, false); err != nil {
		t.Fatal(err)
	}
}
