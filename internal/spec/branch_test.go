package spec

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.email=a@b", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Decision: `sdkit spec branch <id>` finds a spec that lives only on its own branch (its pull request is open and this tree is on the
// base), says where it is when another folder holds that branch, and still refuses a spec that does not exist anywhere. update-specs
// starts with this command, so a refusal here stops every update begun outside the spec's own tree.
func TestSpecBranchFromAnotherTree(t *testing.T) {
	setup := func(t *testing.T) string {
		root := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(filepath.Join(root, ".sdkit"), 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, root, "init", "-q", "-b", "main")
		os.WriteFile(filepath.Join(root, ".sdkit", "sdkit.env"), []byte("BASE_BRANCH=main\n"), 0o644)
		git(t, root, "add", ".")
		git(t, root, "commit", "-q", "-m", "init")
		git(t, root, "switch", "-q", "-c", "spec/x")
		os.MkdirAll(filepath.Join(root, "specs", "x"), 0o755)
		os.WriteFile(filepath.Join(root, "specs", "x", "spec.md"), []byte("x\n"), 0o644)
		git(t, root, "add", ".")
		git(t, root, "commit", "-q", "-m", "spec x")
		git(t, root, "switch", "-q", "main")
		return root
	}
	run := func(root string, id string) (int, string) {
		var out, errb bytes.Buffer
		code := RunSpecAt(root, []string{"branch", id}, &out, &errb)
		return code, out.String() + errb.String()
	}
	branch := func(t *testing.T, root string) string {
		c := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		c.Dir = root
		b, _ := c.Output()
		return strings.TrimSpace(string(b))
	}

	t.Run("the spec is only on its branch: the branch is brought here", func(t *testing.T) {
		root := setup(t)
		if code, msg := run(root, "x"); code != 0 {
			t.Fatalf("exit %d: %s", code, msg)
		}
		if b := branch(t, root); b != "spec/x" {
			t.Errorf("on %q, want spec/x", b)
		}
		if _, err := os.Stat(filepath.Join(root, "specs", "x", "spec.md")); err != nil {
			t.Error("the spec's files are not here")
		}
	})
	t.Run("another folder holds the branch: say where, change nothing", func(t *testing.T) {
		root := setup(t)
		wt := filepath.Join(filepath.Dir(root), "repo-wt", "spec", "x")
		git(t, root, "worktree", "add", "-q", wt, "spec/x")
		code, msg := run(root, "x")
		// the path may come back with the other slash, or the long form of a short Windows name: the folder's own tail is what is compared
		if code == 0 || !strings.Contains(filepath.ToSlash(msg), "repo-wt/spec/x") || !strings.Contains(msg, "another folder") {
			t.Errorf("exit %d, message %q: want a refusal naming the folder that holds spec/x", code, msg)
		}
		if b := branch(t, root); b != "main" {
			t.Errorf("the tree moved to %q", b)
		}
	})
	t.Run("no such spec anywhere: refused, no branch made", func(t *testing.T) {
		root := setup(t)
		if code, msg := run(root, "nope"); code == 0 || !strings.Contains(msg, "no spec directory") {
			t.Errorf("exit %d, message %q", code, msg)
		}
		c := exec.Command("git", "show-ref", "--verify", "-q", "refs/heads/spec/nope")
		c.Dir = root
		if c.Run() == nil {
			t.Error("a branch was made for a spec that does not exist")
		}
	})
}
