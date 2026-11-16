package plan

import (
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func waves(tasks string) string {
	ts, _ := ParseTasks(tasks)
	out, err := Waves(ts, 3)
	if err != nil {
		out = append(out, err.Error())
	}
	return strings.Join(out, "\n")
}

func TestTables(t *testing.T) {
	b, _ := os.ReadFile("../../testdata/golden/plan/tasks.md")
	testutil.Golden(t, "plan/waves", "exit: 0\n"+waves(string(b))+"\n")
	bad, _, _ := CheckWrites("- [ ] T1 | writes: a/, b.go", "T1", []string{"a/x", "b.go", "c.go", "ab"})
	dod, _ := CheckDoD("D1: a | Verified by: none\nD1: b\nD2: | Verified by: unit test")
	par, _ := Parity("D1: a | Verified by: test", "D1: b | Verified by: test", "", false, false)
	for got, want := range map[string]string{
		waves("- [ ] A | writes: x/\n- [ ] B | writes: y/"):     "wave 1: A  ||  B",
		waves("- [ ] A | writes: x/\n- [ ] B | writes: x/f.go"): "wave 1: A\nwave 2: B",
		waves("- [ ] A | after: B\n- [ ] B"):                    "wave 1: B\nwave 2: A",
		waves("- [ ] A | after: B\n- [ ] B | after: A"):         "dependency cycle among remaining tasks",
		strings.Join(bad, ","):                                  "c.go,ab",
		strings.Join(dod, "|"):                                  "D1: \"none\" needs a reason (none: <reason>)|duplicate id D1|D1: no \"Verified by:\" method|D2: empty condition",
		strings.Join(par, "|"):                                  "DoD differs (< plan, > proposal):|  < D1|a|test|  > D1|b|test",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
