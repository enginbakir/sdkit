package capped_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestKeepsFailureLinesAndExit(t *testing.T) {
	r := testutil.TempRepo(t)
	script := "i=0\nwhile [ $i -lt 30 ]; do echo \"line $i\"; i=$((i+1)); done\necho \"FAIL: boom\"\necho \"line after\"\nexit 3\n"
	_ = os.WriteFile(filepath.Join(r, "noisy.sh"), []byte(script), 0o644)
	out, code := testutil.RunKit(t, r, "", "capped", "--lines", "6", "--", "sh", "./noisy.sh")
	if code != 3 {
		t.Errorf("exit %d, want 3", code)
	}
	testutil.Golden(t, "capped/fail", "exit: 3\n"+regexp.MustCompile(`cmd-\d+-\d+-\d+\.log`).ReplaceAllString(out, "cmd-<LOG>.log"))
}
