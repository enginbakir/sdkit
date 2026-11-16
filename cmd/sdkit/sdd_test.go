package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/testutil"
)

// TestSDDGolden replays the "# sh:" and "# sdd:" steps recorded in each golden (recorded against the bash sdd.sh).
func TestSDDGolden(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "sdkit")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %s", out)
	}
	files, _ := filepath.Glob("../../testdata/golden/sdd/*.txt")
	for _, f := range files {
		repo, got := testutil.TempRepo(t), ""
		run := func(cmd string) (string, int) {
			r, _ := proc.RunShell(context.Background(), repo, []string{"CLAUDE_PROJECT_DIR=" + repo, "VERIFY_BASE=main", "SDDCMD=" + bin + " sdd"}, cmd)
			return r.Stdout + r.Stderr, r.Code
		}
		prelude(t, repo)
		b, _ := os.ReadFile(f)
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "# sh: ") {
				run(l[6:])
			} else if strings.HasPrefix(l, "# sdd: ") {
				out, code := run("$SDDCMD " + l[7:] + " 2>&1")
				got += "$ sdd " + l[7:] + "\n" + out + "exit: " + strconv.Itoa(code) + "\n"
			}
		}
		testutil.Golden(t, "sdd/"+strings.TrimSuffix(filepath.Base(f), ".txt"), got)
	}
}

// prelude makes a repo on a feature branch with one base commit, a proposal with a Definition of Done, and a settings file.
func prelude(t *testing.T, repo string) {
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q", "-b", "main", ".")
	for f, c := range map[string]string{
		".sdkit/sdkit.env": "BASE_BRANCH=main\nSDD_MODE=strict\n", ".sdkit/.gitignore": "work/\n.sdd/\n",
		"src/app.go": "base\n", "README.md": "base\n",
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(repo, f)), 0o755)
		_ = os.WriteFile(filepath.Join(repo, f), []byte(c), 0o644)
	}
	git("add", "-A")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "feat")
	_ = os.MkdirAll(filepath.Join(repo, "docs"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, "docs/proposal.md"), []byte("# P\n## Definition of Done\n- D1: it works | Verified by: unit test\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "docs/empty.md"), []byte("# P\nno list here\n"), 0o644)
}
