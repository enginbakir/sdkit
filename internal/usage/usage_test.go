package usage

import (
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestUsage(t *testing.T) {
	t.Chdir("../../testdata/golden/usage")
	full, na := Compute([]string{"full.jsonl"}, Options{Scope: "task full", Prices: "pricing.json"}), Compute([]string{"na.jsonl"}, Options{Scope: "task na", Prices: "pricing.json"})
	testutil.Golden(t, "usage/report", full.Text()+na.Text())
	if na.Rows[1].TQ != "unavailable" || na.Rows[1].Cost != nil || na.Cache.Read*100 != 32*na.Cache.InputSide {
		t.Errorf("an unmeasured run has no cost, and the hit rate is cache_read / (in+read+write): %+v", na.Rows[1])
	}
	f := t.TempDir() + "/u.jsonl"
	for _, l := range []Line{{ID: "a", Kind: "final", In: 5}, {ID: "a", Kind: "snapshot", In: 9}, {ID: "a", Kind: "final", TQ: "unavailable"}, {ID: "b", Kind: "final", In: 1}, {ID: "b", Kind: "final", Out: 3}} {
		_ = Log(f, l)
	}
	if g := ReadLines(f); len(g) != 2 || g[0].In != 5 || g[1].In != 1 || g[1].Out != 3 {
		t.Errorf("dedupe keeps the final and the larger counters: %+v", g)
	}
}
