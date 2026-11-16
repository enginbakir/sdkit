package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestReportAndCache(t *testing.T) {
	repo := testutil.TempRepo(t)
	// Fixture: generic stack, one commit, one new file, two policies (one warns, one fails) and a skipped helper.
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	vd := filepath.Join(repo, ".sdkit/verify.d")
	_ = os.MkdirAll(vd, 0o755)
	for f, c := range map[string]string{
		"../sdkit.env":      "STACK=generic\nVERIFY_FAST_CMD=true\n",
		"10-warn.sh":        "#!/bin/sh\necho \"WARN: $(printf '%s' \"$VERIFY_CHANGED\" | grep -c .) changed file(s)\"\n",
		"20-forbidden.sh":   "#!/bin/sh\necho \"FAIL: forbidden pattern in new.txt\"\nexit 1\n",
		"_helper.sh":        "echo never\n",
		"30-off.sh.example": "exit 1\n",
		"../../README":      "base\n",
	} {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(f, ".sh") {
			mode = 0o755
		}
		_ = os.WriteFile(filepath.Join(vd, f), []byte(c), mode)
	}
	git("add", "-A")
	git("commit", "-qm", "init")
	_ = os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644)
	var a, b bytes.Buffer
	code := Run(context.Background(), repo, Options{NoCache: true}, &a)
	testutil.Golden(t, "verify/report", fmt.Sprintf("exit: %d\n%s", code, a.String()))
	os.Remove(filepath.Join(repo, ".sdkit/verify.d/20-forbidden.sh"))
	Run(context.Background(), repo, Options{}, &a)
	if code = Run(context.Background(), repo, Options{}, &b); code != 0 || !strings.Contains(b.String(), "(cached") {
		t.Fatal(code, b.String())
	}
}
