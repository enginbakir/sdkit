package tasks

import (
	"fmt"
	"strings"
	"testing"
)

func TestCore(t *testing.T) {
	d, _ := Parse([]byte(`{"specId":"s1","tasks":[{"ref":"A","acceptanceCriteria":["AC-2","AC-1"],"scope":["src/b/","src/a/"],"dependsOn":["B"]},{"ref":"B","acceptanceCriteria":["AC-3"],"scope":["x"],"slug":"cs","change":"c1","dependsOn":["A"]}]}`))
	if err := d.Keys(); err != nil || d.Tasks[0].ExternalID != "s1:t739b2ae1f7" || d.Tasks[1].ExternalID != "s1:t399fafc96b" {
		t.Fatalf("ids %v %v", d.Tasks, err) // oracle hashes, incl. the collapsed empty slug of A
	}
	if _, cyc := d.Cycles(); cyc != "A -> B -> A" {
		t.Fatalf("cycle %q", cyc)
	}
	if _, p := d.Trace([]string{"AC-1", "AC-4"}); len(p) != 2 {
		t.Fatalf("trace %v", p)
	}
}

func TestPlanCheck(t *testing.T) {
	// 39 tasks, 7 of them single-AC, the rest 2-14 ACs: the shape of the LOGIN-V2 plan that should have been 12-20 tasks
	sizes := []int{1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 4, 4, 4, 5, 5, 5, 5, 6, 6, 7, 8, 12, 14}
	var b strings.Builder
	b.WriteString(`{"specId":"s","tasks":[`)
	n := 0
	for i, sz := range sizes {
		var acs []string
		for k := 0; k < sz; k++ {
			n++
			acs = append(acs, fmt.Sprintf("%q", fmt.Sprintf("AC-S-%d", n)))
		}
		dep := ""
		if i > 0 {
			dep = fmt.Sprintf(`,"dependsOn":["T%d"]`, i-1)
		}
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"ref":"T%d","risk":"medium","acceptanceCriteria":[%s],"scope":["app/form/"]%s}`, i, strings.Join(acs, ","), dep)
	}
	b.WriteString("]}")
	d, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	text := func(ps []Advice) string {
		var s []string
		for _, a := range ps {
			s = append(s, a.Text)
		}
		return strings.Join(s, "\n")
	}
	got := text(d.PlanCheck(DefaultPlanCfg(), nil, "## Risk rubric\n| `high` | TODO |\n"))
	// 124 ACs at 4-12 per task allow 11-31 tasks; the configured 12-20 narrows it
	if !strings.Contains(got, "aim for 12-20") || !strings.Contains(got, "7 tasks carry a single AC") || !strings.Contains(got, "risk rubric") ||
		!strings.Contains(got, "merge:") || !strings.Contains(got, "chain:") {
		t.Fatalf("missing advice:\n%s", got)
	}
	if !d.PlanCheck(DefaultPlanCfg(), nil, "## Risk rubric\nTODO")[0].Error {
		t.Fatal("an unfilled rubric is an error, not a warning")
	}
	g := GateMarkers("#### Scenario: AC-S-1 (pending Q1, conditional Q3)\n#### Scenario: AC-S-2 (withdrawn)\n#### Scenario: AC-S-3 (note)\n", []string{"pending", "conditional"})
	if len(g) != 1 || strings.Join(g["AC-S-1"], ",") != "Q1,Q3" {
		t.Fatalf("gates %v", g)
	}
}

func TestMerge(t *testing.T) {
	d, _ := Parse([]byte(`{"specId":"s","specSha":"x","tasks":[
	 {"ref":"A","risk":"low","acceptanceCriteria":["AC-1"],"scope":["a/"],"summary":"sa"},
	 {"ref":"B","risk":"high","acceptanceCriteria":["AC-2"],"scope":["a/"],"dependsOn":["A"],"summary":"sb"},
	 {"ref":"C","risk":"low","acceptanceCriteria":["AC-3"],"dependsOn":["B"]}]}`))
	if err := d.Merge([]string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	m := d.Tasks[0]
	if len(d.Tasks) != 2 || strings.Join(m.ACs, ",") != "AC-1,AC-2" || Str(m.Risk) != "high" || len(m.DependsOn) != 0 || Str(m.Summary) != "sa\n\nsb" || d.Tasks[1].DependsOn[0] != "A" {
		t.Fatalf("%+v", d.Tasks)
	}
	// A and C cannot merge when B (between them) stays outside: C depends on B depends on A
	d2, _ := Parse([]byte(`{"specId":"s","tasks":[{"ref":"A","acceptanceCriteria":["AC-1"]},{"ref":"B","acceptanceCriteria":["AC-2"],"dependsOn":["A"]},{"ref":"C","acceptanceCriteria":["AC-3"],"dependsOn":["B"]}]}`))
	if err := d2.Merge([]string{"A", "C"}); err == nil || len(d2.Tasks) != 3 {
		t.Fatalf("a merge that makes a cycle must be refused and leave the plan alone: %v", err)
	}
}
