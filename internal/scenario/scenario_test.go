package scenario

import "testing"

// A: standalone, low risk, intake to done; a code edit before RED is refused.
func TestScenarioA(t *testing.T) {
	t.Parallel()
	repo := kit(t, "VERIFY_CMD=true\n")
	edit := pre("Edit", "file_path", "src/a.go")
	gate(t, repo, edit, true)
	sd(t, repo, 0, "sdd", "start", "x", "--risk", "low", "--proposal", "docs/p/proposal.md")
	has(t, sd(t, repo, 0, "sdd", "status"), "phase=intake")
	write(t, repo, "docs/p/proposal.md", "# P\n## Definition of Done\n- D1: works | Verified by: a_test.go\n")
	sd(t, repo, 0, "sdd", "advance", "proposed")
	gate(t, repo, edit, true)
	gate(t, repo, pre("Write", "file_path", "a_test.go"), false)
	sd(t, repo, 1, "sdd", "advance", "red", "--test-cmd", "true") // passing tests prove nothing
	sd(t, repo, 0, "sdd", "advance", "red", "--test-cmd", "test -f impl.txt")
	gate(t, repo, edit, false)
	sd(t, repo, 1, "sdd", "advance", "green") // implementation missing
	write(t, repo, "impl.txt", "1")
	sd(t, repo, 0, "sdd", "advance", "green")
	push := pre("Bash", "command", "git push origin feat")
	gate(t, repo, push, true)
	sd(t, repo, 0, "sdd", "advance", "verified") // VERIFY_CMD passes
	gate(t, repo, push, false)
	sd(t, repo, 0, "guard", "pre-push")
	sd(t, repo, 0, "sdd", "advance", "pr")
	sd(t, repo, 0, "sdd", "advance", "done")
	has(t, sd(t, repo, 0, "sdd", "status"), "no active task")
}

// B: medium and high risk evidence, stale verdicts, risk floor, and the CI gate reading the base's rules.
func TestScenarioB(t *testing.T) {
	t.Parallel()
	push := pre("Bash", "command", "git push origin feat")
	spawned := func(repo, role, run string) {
		sd(t, repo, 0, "sdd", "_spawn", role, "--source", "hook", "--run", run)
		has(t, sd(t, repo, 0, "sdd", "_evidence", role, "PASS", "--source", "hook", "--run", run), "trust=spawned")
	}

	repo := kit(t, "")
	toGreen(t, repo, "medium")
	sd(t, repo, 1, "sdd", "advance", "verified") // no independent verdict yet
	gate(t, repo, push, true)
	sd(t, repo, 1, "guard", "pre-push")
	spawned(repo, "verifier", "r1")
	sd(t, repo, 0, "sdd", "advance", "verified")
	gate(t, repo, push, false)
	if out, ok := ci(t, repo); !ok {
		t.Fatalf("ci-check must accept the attested diff: %s", out)
	}
	write(t, repo, "impl.txt", "2") // an edit after the verdict
	has(t, sd(t, repo, 0, "sdd", "status"), "evidence=stale")
	gate(t, repo, push, true)
	sd(t, repo, 1, "sdd", "advance", "verified")
	write(t, repo, ".sdkit/sdkit.env", "SDD_MODE=off\nSDD_FREE_GLOBS=*\n") // the PR loosens the rules that judge it
	if out, ok := ci(t, repo); ok {
		t.Fatalf("ci-check must use the base's rules and fail: %s", out)
	}

	repo = kit(t, "")
	toGreen(t, repo, "high")
	spawned(repo, "verifier", "r1")
	sd(t, repo, 1, "sdd", "advance", "verified") // reviewer and security-auditor missing
	sd(t, repo, 0, "sdd", "_evidence", "reviewer", "PASS")
	sd(t, repo, 0, "sdd", "_evidence", "security-auditor", "PASS")
	has(t, sd(t, repo, 0, "sdd", "evidence"), "untrusted")
	sd(t, repo, 1, "sdd", "advance", "verified") // hand-recorded evidence does not count
	spawned(repo, "reviewer", "r2")
	spawned(repo, "security-auditor", "r3")
	sd(t, repo, 0, "sdd", "advance", "verified")

	repo = kit(t, "RISK_FLOOR_GLOBS=high:secure/*\n")
	toRed(t, repo, "low")
	write(t, repo, "impl.txt", "1")
	write(t, repo, "secure/k.go", "package s\n")
	sd(t, repo, 1, "sdd", "advance", "green") // the label is below the floor of the diff
	sd(t, repo, 0, "sdd", "abort")
	sd(t, repo, 0, "sdd", "start", "t2", "--risk", "high", "--proposal", "docs/p/proposal.md") // only raised
}
