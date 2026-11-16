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

// A begin on content that already has its evidence must not use a round when REVIEW_BEGIN_IDEMPOTENT is on;
// off (the default) keeps the old counting. A NOT_RUN round learned nothing and may be repeated.
func TestBeginIdempotent(t *testing.T) {
	steps := []struct {
		name    string
		act     func(c Ctx, doc string)
		rounds  int // expected state round after the step
		changes bool
	}{
		{"first begin", func(c Ctx, d string) { c.Begin("s", d, false) }, 1, false},
		{"begin again while open", func(c Ctx, d string) { c.Begin("s", d, false) }, 1, false},
		{"record PASS then begin, same content", func(c Ctx, d string) { c.Record("s", "PASS", "fresh", d+".f"); c.Begin("s", d, false) }, 1, false},
		{"content changed", func(c Ctx, d string) { os.WriteFile(d, []byte("changed"), 0o644); c.Begin("s", d, false) }, 2, true},
		{"NOT_RUN may be repeated", func(c Ctx, d string) { c.Record("s", "NOT_RUN", "none", ""); c.Begin("s", d, false) }, 3, true},
	}
	for _, on := range []bool{true, false} {
		root := t.TempDir()
		doc := filepath.Join(root, "s.md")
		os.WriteFile(doc, []byte("v1"), 0o644)
		os.WriteFile(doc+".f", []byte("[]"), 0o644)
		c := Load(root)
		c.MaxRounds, c.IdempotentBegin = 9, on
		var got []int
		for _, s := range steps {
			s.act(c, doc)
			st, _ := readState(c.stateFile("s"))
			got = append(got, st.Round)
		}
		want := []int{1, 1, 1, 2, 3}
		if !on {
			want = []int{1, 2, 3, 4, 5} // before: every begin burns a round
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("idempotent=%v: rounds %v, want %v", on, got, want)
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
