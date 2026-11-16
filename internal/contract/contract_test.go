package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContract(t *testing.T) {
	r := t.TempDir()
	os.MkdirAll(filepath.Join(r, ".sdkit"), 0o755)
	os.WriteFile(filepath.Join(r, ".sdkit", "sdkit.env"), nil, 0o644)
	c, h := New(r), "CONTRACT: 1\nID: t/a\nROLE: verifier\nTASK: x\nSCOPE: writes: none\n"
	ans := func(a string) Result {
		return c.CheckResult("HANDOFF-RESULT/1 | id: t/a | role: verifier | status: done | artifacts: "+a+" | blockers: none | next: x", Options{Record: true})
	}
	if c.Issue("CONTRACT: 1\nID: t/b\n", Options{}).Valid || !c.Issue(h, Options{}).Valid || c.Issue(h, Options{}).Reason != "duplicate" {
		t.Error("handoff")
	}
	if ans("x.go").Reason != "inconsistent" || c.CheckResult("done", Options{}).Valid || !ans("none").Valid || ans("none").Reason != "duplicate" {
		t.Error("answer")
	}
}
