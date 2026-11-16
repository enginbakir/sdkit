package scenario

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	c.Dir = repo
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

func write(t *testing.T, repo, rel, body string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
		t.Fatal("write", rel)
	}
}

// kit is a standalone kit project with its base commit on main, checked out on branch feat.
func kit(t *testing.T, env string) string {
	if runtime.GOOS == "windows" {
		t.Skip("scenarios use sh test commands")
	}
	repo := testutil.TempRepo(t)
	write(t, repo, ".sdkit/sdkit.env", "SDKIT_ENV=standalone\nBASE_BRANCH=main\nSTACK=generic\n"+env)
	write(t, repo, ".gitignore", ".sdkit/.sdd/\n")
	git(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "base")
	git(t, repo, "checkout", "-qb", "feat")
	return repo
}

// sd runs `sdkit <args>`, fails the test unless it exits with want, and returns stdout.
func sd(t *testing.T, repo string, want int, args ...string) string {
	t.Helper()
	out, code := testutil.RunKit(t, repo, "", args...)
	if code != want {
		t.Fatalf("sdkit %v: exit %d, want %d\n%s", args, code, want, out)
	}
	return out
}

func has(t *testing.T, got, sub string) {
	t.Helper()
	if !strings.Contains(got, sub) {
		t.Fatalf("want %q in %q", sub, got)
	}
}

func pre(tool, key, val string) string {
	return fmt.Sprintf(`{"tool_name":%q,"tool_input":{%q:%q},"hook_event_name":"PreToolUse"}`, tool, key, val)
}

func hook(t *testing.T, repo, host, stdin string) (string, int) {
	t.Helper()
	return testutil.RunKit(t, repo, stdin, "hook", "--host", host, "PreToolUse")
}

// gate fails the test unless the claude hook denies (or, with deny false, silently allows) the tool call.
func gate(t *testing.T, repo, in string, deny bool) {
	t.Helper()
	if out, _ := hook(t, repo, "claude", in); (out != "") != deny {
		t.Fatalf("deny=%v, hook said %q for %s", deny, out, in)
	}
}

// toRed opens task "t" and walks it to RED (the test fails until impl.txt exists).
func toRed(t *testing.T, repo, risk string) {
	write(t, repo, "docs/p/proposal.md", "# P\n## Definition of Done\n- D1: works | Verified by: a_test.go\n")
	write(t, repo, "docs/p/design.md", "## Race\nnone\n")
	sd(t, repo, 0, "sdd", "start", "t", "--risk", risk, "--proposal", "docs/p/proposal.md")
	sd(t, repo, 0, "sdd", "advance", "proposed")
	write(t, repo, "a_test.go", "package a\n")
	sd(t, repo, 0, "sdd", "advance", "red", "--test-cmd", "test -f impl.txt")
}

func toGreen(t *testing.T, repo, risk string) {
	toRed(t, repo, risk)
	write(t, repo, "impl.txt", "1")
	sd(t, repo, 0, "sdd", "advance", "green")
}

// ci runs `sdkit sdd ci-check` the way a CI job does: VERIFY_BASE=main in the environment (RunKit strips it).
func ci(t *testing.T, repo string) (string, bool) {
	t.Helper()
	bin := binOnce()
	c := exec.Command(bin, "sdd", "ci-check")
	c.Dir = repo
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_") && !strings.HasPrefix(kv, "SDKIT_") && !strings.HasPrefix(kv, "SDD_") && !strings.HasPrefix(kv, "VERIFY_") {
			c.Env = append(c.Env, kv)
		}
	}
	c.Env = append(c.Env, "VERIFY_BASE=main")
	out, err := c.CombinedOutput()
	return string(out), err == nil
}

var binOnce = sync.OnceValue(func() string {
	p := filepath.Join(os.TempDir(), fmt.Sprintf("sdkit-scn-%d", os.Getpid()))
	if out, err := exec.Command("go", "build", "-o", p, "../../cmd/sdkit").CombinedOutput(); err != nil {
		panic(string(out))
	}
	return p
})
