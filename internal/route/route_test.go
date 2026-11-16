package route

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestDecide(t *testing.T) {
	var gold strings.Builder
	for i, row := range []string{ // role risk evidence model tokens-used failed-attempts | action reason tier exit
		"verifier high valid - 0 0 | reuse EVIDENCE_REUSED - 7", "developer medium - - 0 2 | escalate ESCALATE_INVALID_OUTPUT strong 0", "test-writer medium - - 150 0 | stop BUDGET_EXHAUSTED standard 3", "verifier high missing - 80 0 | execute RISK_FLOOR_APPLIED strong 0",
		"verifier low - haiku 0 0 | stop OVERRIDE_BELOW_FLOOR - 2", "reviewer medium - - 80 0 | stop OPTIONAL_SKIPPED standard 8", "security-auditor high block - 0 0 | stop SECURITY_BLOCK - 8", "developer low - - 0 4 | stop RETRIES_EXHAUSTED - 8",
	} {
		f, want, _ := strings.Cut(row, " | ")
		a, n, in := strings.Fields(f), 0, KitInput(func(k string) string {
			return map[string]string{"RUN_ROLE_HOST": "claude", "TOKEN_BUDGET_TASK": "100", "ROUTING": "on"}[k]
		}, nil, nil)
		fmt.Sscan(a[4]+" "+a[5], &in.Use.Tokens, &n)
		in.Slug, in.Role, in.Risk, in.Mandatory, in.Use.N, in.Model, in.RiskSource, in.Ev = "t1", a[0], a[1], a[1] == "high", 1, strings.Trim(a[3], "-"), map[bool]string{true: "flag"}[a[2] == "-"], Evidence{Known: a[2] != "-", State: a[2], Run: "r1", BlockedHere: a[2] == "block"}
		for j := range n { // failed attempts: a decision and its INVALID_OUTPUT outcome each
			in.Chain = append(in.Chain, Entry{Type: "decision", ID: fmt.Sprint("d", j+1), Action: []string{"execute", "retry"}[min(j, 1)], Adapter: "claude", Model: "sonnet", Tier: "standard", Enforced: true}, Entry{Type: "outcome", ID: fmt.Sprint("d", j+1), Class: "INVALID_OUTPUT"})
		}
		d := Decide(in)
		if got := fmt.Sprint(d.Action, " ", d.Reason, " ", orDef(d.Tier, "-"), " ", d.Exit); got != want {
			t.Errorf("row %d: got %q, want %q", i, got, want)
		}
		if j, _ := d.MarshalJSON(); i < 3 {
			fmt.Fprintf(&gold, "== %s exit %d\n%s\n", d.Action, d.Exit, j)
		}
	}
	testutil.Golden(t, "route/decide", gold.String())
}
