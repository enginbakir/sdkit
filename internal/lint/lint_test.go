package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One row per mechanical mistake the LOGIN-V2 review rediscovered round after round.
func TestRun(t *testing.T) {
	cfg := Config{GatePrefix: "Q", MarkerWords: []string{"pending", "conditional"}, ClosedWords: []string{"closed"}, RefPrefixes: []string{"D", "R"}}
	base := map[string]string{
		"spec.md":  "#### Scenario: AC-X-1\n#### Scenario: AC-X-2 (pending Q1)\n#### Scenario: AC-X-3 (withdrawn)\n#### Scenario: AC-X-4\n",
		"prd.md":   "| Q1 | what? | owner | AC-X-2, 4 |\n| Q2 | closed: done | - | - |\n- **R-1** rule. AC-X-1\n### D1 - decision\nSee D1 and R-1.\n",
		"tasks.md": "- [ ] 1.1 AC-X-1, 2, 4\n",
	}
	cases := []struct{ name, file, from, to, want string }{
		{"clean", "", "", "", ""},
		{"duplicate AC", "spec.md", "AC-X-4\n", "AC-X-1\n", "ac-duplicate"},
		{"AC used, never defined", "prd.md", "See D1", "See AC-X-9 D1", "ac-undefined"},
		{"shorthand list expands", "tasks.md", "AC-X-1, 2, 4", "AC-X-1, 2, 7", "ac-undefined"},
		{"tasks.md misses an AC", "tasks.md", "AC-X-1, 2, 4", "AC-X-1, 2", "tasks-coverage"},
		{"withdrawn AC needs no task", "tasks.md", "AC-X-1, 2, 4", "AC-X-1, 2, 4", ""},
		{"marker names a closed question", "spec.md", "(pending Q1)", "(pending Q2)", "gate-closed"},
		{"marker names an unknown question", "spec.md", "(pending Q1)", "(pending Q9)", "gate-undefined"},
		{"row does not list the marked AC", "prd.md", "AC-X-2, 4", "AC-X-4", "gate-unlisted"},
		{"undefined reference", "prd.md", "See D1 and R-1.", "See D7 and R-1.", "ref-undefined"},
		{"gap without withdrawn", "spec.md", "#### Scenario: AC-X-4", "#### Scenario: AC-X-6", "ac-gap"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		for n, b := range base {
			if n == c.file {
				if !strings.Contains(b, c.from) {
					t.Fatalf("%s: fixture lacks %q", c.name, c.from)
				}
				b = strings.Replace(b, c.from, c.to, 1)
			}
			_ = os.WriteFile(filepath.Join(dir, n), []byte(b), 0o644)
		}
		ps, err := Run(dir, cfg)
		if err != nil {
			t.Fatal(err)
		}
		var codes []string
		for _, p := range ps {
			codes = append(codes, p.Code)
		}
		got := strings.Join(codes, " ")
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got [%s], want %q", c.name, got, c.want)
		}
	}
}
