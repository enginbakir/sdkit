package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestStateShow(t *testing.T) {
	r := testutil.TempRepo(t)
	os.MkdirAll(filepath.Join(r, ".sdkit", ".sdd"), 0o755)
	os.WriteFile(filepath.Join(r, ".sdkit", "sdkit.env"), nil, 0o644)
	for _, a := range []string{"init medium feat/x", "set step 3-implement", "set verdict PASS", "task T1 done", "task T2 doing", "retry T1", "spawn"} {
		testutil.RunKit(t, r, "", append([]string{"state", "t"}, strings.Fields(a)...)...)
	}
	if _, code := testutil.RunKit(t, r, "", "state", "t", "retry", "T1"); code != 1 {
		t.Errorf("second retry exit %d, want 1", code)
	}
	out, _ := testutil.RunKit(t, r, "", "state", "t", "show")
	testutil.Golden(t, "state/show", "exit: 0\n"+out)
	got := "exit: 0\n"
	for _, c := range [][2]string{{"low", "standard"}, {"medium", "standard"}, {"high", "high-assurance"}} {
		os.WriteFile(filepath.Join(r, ".sdkit", ".sdd", "state.json"), []byte(`{"task":"t","risk":"`+c[0]+`","profile":"`+c[1]+`"}`), 0o644)
		a, _ := testutil.RunKit(t, r, "", "workflow", "policy")
		b, _ := testutil.RunKit(t, r, "", "workflow", "policy", "--json")
		got += "== " + c[0] + " " + c[1] + "\n" + a + b
	}
	testutil.Golden(t, "workflow/policy", got)
}
