package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSessionID(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"init line", `{"type":"system","subtype":"init","session_id":"02031485-a61e-4cbc-89b5-3986a8b7289a"}`, "02031485-a61e-4cbc-89b5-3986a8b7289a"},
		{"line without one", `{"type":"assistant","message":{}}`, ""},
		{"not a session id (it becomes a file name)", `{"session_id":"../../x"}`, ""},
		{"not json", `session_id garbage`, ""},
	}
	for _, c := range cases {
		if got := sessionID([]byte(c.line)); got != c.want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

func storedReport(t *testing.T, root, id, last string, tokens int, cost float64, state string) {
	dir := filepath.Join(root, ".sdkit", "reports")
	os.MkdirAll(dir, 0o755)
	body := `{"schema":"value-report/1","kind":"session","session":{"id":"` + id + `","state":"ended","first_record":"2026-11-17T08:00:00Z","last_record":"` + last + `"},` +
		`"tokens":{"total":` + itoa(tokens) + `,"state":"complete"},"cost":{"api_equivalent_usd":` + ftoa(cost) + `,"state":"` + state + `"},` +
		`"roles":[{"role":"orchestrator","kind":"orchestrator","model":"m","runs":1,"tokens":` + itoa(tokens) + `,"cost_usd":` + ftoa(cost) + `}],"warnings":[],"summary":"s"}`
	os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o644)
}

func TestReportsAreLinkedOnlyThroughTheRunLedger(t *testing.T) {
	root := scratchRepo(t, "/.sdkit/work/\n")
	a, b, c := "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa", "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb", "cccccccc-3333-4333-8333-cccccccccccc"
	storedReport(t, root, a, "2026-11-17T09:00:00Z", 1000, 1.5, "complete")
	storedReport(t, root, b, "2026-11-18T09:00:00Z", 500, 0.5, "partial")
	storedReport(t, root, c, "2026-11-19T09:00:00Z", 200, 0.2, "estimated") // started in a terminal, not by the panel
	RecordRun(root, RunRecord{Session: a, Spec: "odeme", Action: "generate-specs", Start: "2026-11-17T08:00:00Z"})
	RecordRun(root, RunRecord{Session: a, Spec: "odeme", Action: "generate-specs", End: "2026-11-17T09:00:00Z", Outcome: "done"})
	RecordRun(root, RunRecord{Session: b, Spec: "odeme", Action: "update-specs", Start: "2026-11-18T08:00:00Z"}) // the panel died before the end line

	if runs := ReadRuns(root); len(runs) != 2 || runs[0].Outcome != "done" || runs[0].End == "" || runs[1].Outcome != "" {
		t.Fatalf("start and end lines of one session fold into one run: %+v", runs)
	}
	got := ListReports(root)
	if len(got.Sessions) != 3 || got.Sessions[0].ID != c {
		t.Fatalf("newest first: %+v", got.Sessions)
	}
	if got.Sessions[0].Spec != "" {
		t.Errorf("a session the panel did not start must not be given a spec: %+v", got.Sessions[0])
	}
	if got.Sessions[1].Spec != "odeme" || got.Sessions[1].Action != "update-specs" {
		t.Errorf("linked through the ledger: %+v", got.Sessions[1])
	}
	if len(got.Specs) != 1 {
		t.Fatalf("one rollup, for the one spec: %+v", got.Specs)
	}
	r := got.Specs[0]
	if r.Sessions != 2 || r.Tokens != 1500 || r.CostUSD != 2 || !r.Partial {
		t.Errorf("rollup = %+v (a partial session makes the sums lower bounds)", r)
	}
	if rs := SpecReports(root, "odeme"); len(rs) != 2 || SpecReports(root, "baska") == nil || len(SpecReports(root, "baska")) != 0 {
		t.Errorf("spec reports: %v", rs)
	}
}

func TestReportMarkdownRefusesAnythingThatIsNotASessionID(t *testing.T) {
	root := scratchRepo(t, "")
	for _, id := range []string{"../../etc/passwd", "a b", "", "x"} {
		if _, err := ReportMarkdown(root, id); err == nil {
			t.Errorf("%q must be refused", id)
		}
	}
}

func itoa(n int) string     { return strconv.Itoa(n) }
func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }

func TestLowerBound(t *testing.T) {
	for state, want := range map[string]bool{"partial": true, "unavailable": true, "estimated": false, "recorded": false, "complete": false, "": false} {
		if got := lowerBound(state); got != want {
			t.Errorf("lowerBound(%q) = %v", state, got)
		}
	}
}

func TestTaskLedgerLinksSessionsToTheirSpec(t *testing.T) {
	root := scratchRepo(t, "/.sdkit/work/\n")
	x, y := "dddddddd-4444-4444-8444-dddddddddddd", "eeeeeeee-5555-4555-8555-eeeeeeeeeeee"
	storedReport(t, root, x, "2026-11-19T09:00:00Z", 100, 0.1, "complete")
	storedReport(t, root, y, "2026-11-19T10:00:00Z", 100, 0.1, "complete")
	os.MkdirAll(filepath.Join(root, "specs", "odeme"), 0o755) // a spec id; other-task is a development task, not a spec
	line := func(session string) string {
		return `{"v":2,"ts":"2026-11-19T09:00:00Z","role":"orchestrator","tier":"","host":"claude","model":"claude-sonnet-5-5","session":"` + session + `","id":"a","origin":"sdk","kind":"final","in":10,"out":20}` + "\n"
	}
	for slug, sid := range map[string]string{"odeme": x, "other-task": y} {
		os.MkdirAll(filepath.Join(root, ".sdkit", "work", slug), 0o755)
		os.WriteFile(filepath.Join(root, ".sdkit", "work", slug, "usage.jsonl"), []byte(line(sid)), 0o644)
	}
	bySpec := map[string]string{}
	for _, r := range ListReports(root).Sessions {
		bySpec[r.ID] = r.Spec
	}
	if bySpec[x] != "odeme" || bySpec[y] != "" {
		t.Errorf("a session that wrote to a spec's task ledger belongs to that spec, one that wrote to a development task does not: %v", bySpec)
	}
	if v := SpecReportsOf(root, "odeme"); len(v.Sessions) != 1 || len(v.Roles) == 0 || v.Roles[0].Role != "orchestrator" {
		t.Errorf("the spec page gets its session and the roles of its task report: %+v", v)
	}
	if v := SpecReportsOf(root, "yok"); len(v.Roles) != 0 || v.Sessions == nil {
		t.Errorf("a spec with nothing filed under it: %+v", v)
	}
}
