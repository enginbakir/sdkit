package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

// TestGolden replays the "# cmd:" lines of spec.txt (recorded from the bash) on a copy of the fixture.
func TestGolden(t *testing.T) {
	b := filepath.Join(t.TempDir(), "base")
	if err := os.CopyFS(b, os.DirFS("../../testdata/golden/spec/fixture/base")); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir() // spec check works on specs/<id> of a project: bad = the fixture (open changes), ok = without them
	for _, n := range []string{"bad", "ok"} {
		_ = os.CopyFS(filepath.Join(repo, "specs", n), os.DirFS("../../testdata/golden/spec/fixture/base"))
	}
	_ = os.RemoveAll(filepath.Join(repo, "specs", "ok", "changes"))
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	g, _ := os.ReadFile("../../testdata/golden/spec/spec.txt")
	var out strings.Builder
	for _, l := range strings.Split(string(g), "\n") {
		if c, ok := strings.CutPrefix(l, "# cmd: "); ok {
			fmt.Fprintf(&out, "$ %s\n", c)
			code := RunSpec(strings.Fields(strings.TrimPrefix(strings.ReplaceAll(c, "$b", b), "spec ")), &out, &out)
			fmt.Fprintf(&out, "exit: %d\n", code)
		}
	}
	for _, f := range []string{"specs/social/spec.md", "changes/feat-b/change.json"} {
		d, _ := os.ReadFile(filepath.Join(b, f))
		fmt.Fprintf(&out, "$ cat %s\n| %s\n", f, strings.ReplaceAll(strings.TrimSuffix(string(d), "\n"), "\n", "\n| "))
	}
	testutil.Golden(t, "spec/spec", out.String())
}
