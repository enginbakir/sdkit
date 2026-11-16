package hook

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestHosts(t *testing.T) {
	Register(PreToolUse, nil, "boom", func(*Input) Decision { panic("guard bug") })
	Register(PreToolUse, nil, "stub", func(in *Input) Decision {
		return Decision{map[string]Kind{"deny": Deny, "ask": Ask}[in.Command+in.Path], "reason " + in.Command + in.Path, "", ""}
	})
	b, _ := os.ReadFile("../../testdata/golden/hook/adapt.txt")
	var got strings.Builder
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.SplitN(strings.TrimPrefix(l, "# in: "), "|", 3); len(f) == 3 {
			var o, e bytes.Buffer
			c := Run(f[1], PreToolUse, []byte(f[2]), &o, &e)
			fmt.Fprintf(&got, "== %s\nexit: %d\nstdout: %s\nstderr: %s\n", f[0], c, strings.TrimSpace(o.String()), strings.TrimSpace(e.String()))
		}
	}
	testutil.Golden(t, "hook/adapt", got.String())
	if Merge(Decision{}, Decision{Kind: Ask}, Decision{Kind: Deny}).Kind != Deny || Merge(Decision{Kind: Ask}, Decision{}).Kind != Ask || Merge(Decision{}, Decision{}).Kind != Allow || Run("", PreToolUse, []byte("{bad"), os.Stdout, os.Stdout) != 0 {
		t.Error("merge must be deny > ask > allow; malformed JSON must exit 0")
	}
}
