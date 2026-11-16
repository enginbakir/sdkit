package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestCheapPassNeverPublishes(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "s.md")
	os.WriteFile(doc, []byte("v1"), 0o644)
	os.WriteFile(doc+".f", []byte("[]"), 0o644)
	c := Load(root)
	c.Begin("s", doc, false)
	c.Record("s", "PASS", "fresh", doc+".f", RecordOpt{Set: "cheap", Roles: 1})
	if r := c.Check("s"); r.OK() || r.State != "PARTIAL" {
		t.Fatalf("a cheap-set PASS must not be approval: %+v", r)
	}
	c.Begin("s", doc, false)
	c.Record("s", "PASS", "fresh", doc+".f", RecordOpt{Roles: 4})
	if r := c.Check("s"); !r.OK() {
		t.Fatalf("the full set PASSes on the same content: %+v", r)
	}
	if st := c.Status("s"); !strings.Contains(st, "reviewer runs recorded: 5") {
		t.Fatal(st)
	}
}

func TestSettleAndRaiseCapNeedAPerson(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "s.md")
	os.WriteFile(doc, []byte("v1"), 0o644)
	os.MkdirAll(filepath.Join(root, ".sdkit"), 0o755) // the project dir, where the audit trail lives
	c := Load(root)
	c.JiraUser = ""
	c.MaxRounds = 1
	c.Begin("s", doc, false)
	if _, err := c.Settle("s-draft", "cost wording", "decided", ""); err == nil {
		t.Fatal("settle without a person must be refused")
	}
	if _, err := c.RaiseCap("s", 3, "more", ""); err == nil {
		t.Fatal("raise-cap without a person must be refused")
	}
	if _, err := c.Begin("s", doc, false); err == nil {
		t.Fatal("cap of 1 reached")
	}
	c.JiraUser = "ayse"
	if _, err := c.Settle("s-draft", "cost wording", "analyst: out of scope", ""); err != nil {
		t.Fatal(err)
	}
	if p := c.SettledPrompt("s"); !strings.Contains(p, "cost wording") { // the draft and output reviews share one list
		t.Fatalf("prompt %q", p)
	}
	if _, err := c.RaiseCap("s", 1, "x", ""); err == nil {
		t.Fatal("raise-cap must go above the current cap")
	}
	if _, err := c.RaiseCap("s", 3, "analyst agreed", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(doc, []byte("v2"), 0o644)
	if _, err := c.Begin("s", doc, false); err != nil {
		t.Fatalf("the raised cap must apply: %v", err)
	}
}

func TestChangedAndConverged(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "doc")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "a.md"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(d, "b.md"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(root, "f.json"), []byte("[]"), 0o644)
	c := Load(root)
	c.Begin("s", d, false)
	c.Record("s", "BLOCK", "fresh", filepath.Join(root, "f.json"))
	os.WriteFile(filepath.Join(d, "b.md"), []byte("b2"), 0o644)
	l, same, _ := c.Changed("s")
	if len(l) != 1 || l[0] != "changed ./b.md" || same != 1 {
		t.Fatalf("%v %d", l, same)
	}
	n := func(m int) Note { return Note{Major: m} }
	for _, tc := range []struct {
		h    []Note
		want bool
	}{{[]Note{n(30), n(20), n(20)}, true}, {[]Note{n(3), n(1), n(4)}, false}, {[]Note{n(20), n(1)}, false}, {[]Note{n(2), n(0), n(0)}, false}} {
		if Converged(tc.h) != tc.want {
			t.Errorf("%v want %v", tc.h, tc.want)
		}
	}
}

func TestScopeGrowthWarning(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "spec.md")
	os.WriteFile(doc, []byte("#### Scenario: AC-X-1\n"), 0o644)
	os.WriteFile(filepath.Join(root, "f.json"), []byte("[]"), 0o644)
	c := Load(root)
	c.MaxNewACs = 2
	c.Begin("s", doc, false)
	c.Record("s", "BLOCK", "fresh", filepath.Join(root, "f.json"))
	os.WriteFile(doc, []byte("#### Scenario: AC-X-1\n#### Scenario: AC-X-2\n#### Scenario: AC-X-3\n#### Scenario: AC-X-4\n"), 0o644)
	msg, err := c.Begin("s", doc, false)
	if err != nil || !strings.Contains(msg, "SCOPE GROWTH: 3 AC ids") {
		t.Fatalf("%v %q", err, msg)
	}
}
