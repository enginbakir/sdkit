package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/testutil"
)

func TestPrespecRefused(t *testing.T) {
	r, n := testutil.TempRepo(t), func(string) {}
	_ = os.MkdirAll(filepath.Join(r, "prespec"), 0o755)
	_ = os.WriteFile(filepath.Join(r, "prespec", "a.md"), []byte("x"), 0o644)
	if PrespecCheck(r, "a", n, n) != 1 { // not ignored
		t.Fatal("non-ignored accepted")
	}
	_ = os.WriteFile(filepath.Join(r, ".gitignore"), []byte("/prespec/\n"), 0o644)
	_, _ = gitx.Run(r, "add", "-f", "prespec/a.md")
	if PrespecCheck(r, "a", n, n) != 1 { // tracked
		t.Fatal("tracked accepted")
	}
}
