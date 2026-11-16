package tasks

import "testing"

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

func TestWithdrawnIDs(t *testing.T) {
	text := "#### Scenario: AC-X-18 (withdrawn)\n- **WHEN** AC-X-18 is evaluated\n" +
		"#### Scenario: AC-X-22 (withdrawn) - old title\n" +
		"#### Scenario: AC-X-1\n- THEN replaces AC-X-18 and mentions (withdrawn)\n" +
		"#### Scenario: AC-X-5 (koşullu Q3)\n"
	got := withdrawnIDs(text)
	if len(got) != 2 || !got["AC-X-18"] || !got["AC-X-22"] {
		t.Fatalf("withdrawn = %v, want AC-X-18 and AC-X-22 only (prose mentions and other markers do not count)", got)
	}
}
