package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/review"
	"github.com/wecii/sdkit/internal/testutil"
)

func TestPublishNeedsFreshPass(t *testing.T) {
	f := filepath.Join(testutil.TempRepo(t), "specs", "x", "spec.md")
	t.Setenv("CLAUDE_PROJECT_DIR", filepath.Dir(filepath.Dir(filepath.Dir(f))))
	t.Setenv("PR_HOST", "github")
	testutil.FakeCLI(t, "gh", "exit 0")
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	for i, c := range []string{"[]", "[ ]"} { // an empty findings array is also a valid document
		_ = os.WriteFile(f, []byte(c), 0o644)
		if o := new(strings.Builder); RunSpec([]string{"publish", "x", "--title", "T", "--body-file", f}, o, o) != 1 || !strings.Contains(o.String(), []string{"no review evidence", "stale"}[i]) {
			t.Fatal(i, o)
		}
		_, _ = review.Load(os.Getenv("CLAUDE_PROJECT_DIR")).Begin("x", "", false)
		_, _ = review.Load(os.Getenv("CLAUDE_PROJECT_DIR")).Record("x", "PASS", "fresh", f)
	}
}
