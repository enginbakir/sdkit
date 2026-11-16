package sdd

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/testutil"
)

func env(t *testing.T, mode, state string) *Env {
	r := testutil.TempRepo(t)
	os.MkdirAll(r+"/.sdkit/.sdd", 0o755)
	os.WriteFile(r+"/.sdkit/sdkit.env", []byte("SDD_MODE="+mode+"\n"), 0o644)
	os.WriteFile(r+"/.sdkit/.sdd/state.json", []byte(state), 0o644)
	return &Env{Root: r, P: project.Resolve(r), Get: func(k string) string { return map[string]string{"SDD_MODE": mode}[k] }, Out: io.Discard, Err: io.Discard, Tel: nop{}, Rep: nop{}}
}

func TestGateMatrix(t *testing.T) { // phase x path x mode, with the attack paths of tests/gate-security.test.sh
	for _, c := range [][4]string{{"", "src/a.go", "strict", "deny"}, {"", "docs/a.md", "strict", "ok"}, {"", "docs/../src/a.go", "strict", "deny"},
		{"intake", "src/a.go", "strict", "deny"}, {"proposed", "src/a_test.go", "strict", "ok"}, {"proposed", "src/a.go", "strict", "deny"},
		{"red", "src/a.go", "strict", "ok"}, {"green", ".sdkit/./.sdd/state.json", "strict", "deny"}, {"green", ".sdkit/attestations/x.json", "strict", "deny"},
		{"", "src/a.go", "off", "ok"}, {"red", "/elsewhere/../etc/x.go", "strict", "ok"}} {
		e := env(t, c[2], `{"task":"t","phase":"`+c[0]+`"}`)
		if ok, why := e.Gate("edit", c[1]); ok != (c[3] == "ok") {
			t.Errorf("%v: got %v %q", c, ok, why)
		}
	}
}

func TestTransitions(t *testing.T) {
	for _, c := range [][3]string{{"intake", "red", "not-next"}, {"green", "intake", "not-next"}, {"pr", "bogus", "unknown-phase"}, {"done", "done", "not-next"}} {
		if r := (&Env{}).advance(&Task{Phase: c[0]}, c[1], ""); r == nil || r.Reason != c[2] {
			t.Errorf("%v: %v", c, r)
		}
	}
}

func TestEvidenceValidity(t *testing.T) {
	rec := func(f func(*Record)) *Record {
		r := Record{"PASS", "fp", "t", "spawned", "hook", "r1", "", 1}
		f(&r)
		return &r
	}
	for want, r := range map[string]*Record{"missing": nil, "valid": rec(func(*Record) {}), "block": rec(func(r *Record) { r.Result = "BLOCK" }),
		"stale": rec(func(r *Record) { r.Fingerprint = "old" }), "untrusted": rec(func(r *Record) { r.Trust = "manual" }),
		"wrong-task": rec(func(r *Record) { r.Task = "x" }), "conflict": rec(func(r *Record) { r.Run = "" })} {
		tk := &Task{Task: "t", Evidence: map[string]Record{}}
		if r != nil {
			tk.Evidence["verifier"] = *r
		}
		if got := EvidenceTable(tk, "fp", []string{"verifier"})[0].State; got != want {
			t.Errorf("want %s got %s", want, got)
		}
	}
}

func TestFloor(t *testing.T) {
	for _, c := range []struct{ globs, files, level, bad string }{{"high:src/*", "src/a.go docs/b.md", "high", ""}, {"medium:*.sql,high:db/*", "x.sql", "medium", ""},
		{"high:src/*", "docs/b.md", "low", ""}, {"high:", "", "low", "'high:' is not <low|medium|high>:<glob>"}, {"huge:a", "", "low", "'huge:a' is not <low|medium|high>:<glob>"}} {
		if lvl, _ := FloorOf(c.globs, strings.Fields(c.files)); lvl != c.level || FloorValidate(c.globs) != c.bad {
			t.Errorf("%+v: %s", c, lvl)
		}
	}
}

func TestConcurrentAdvance(t *testing.T) {
	e := env(t, "strict", `{"task":"t","risk":"low","proposal":"p.md","phase":"intake"}`)
	os.WriteFile(e.Root+"/p.md", []byte("## Definition of Done\n- D1: x | Verified by: t\n"), 0o644)
	var wg sync.WaitGroup
	won := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); won <- e.Advance("proposed", "") }()
	}
	wg.Wait()
	if a, b := <-won, <-won; a+b != 1 {
		t.Errorf("exit codes %d %d: exactly one advance must win", a, b)
	}
}
