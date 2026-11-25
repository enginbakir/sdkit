package ext

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=a@b", "-c", "user.name=t", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Decisions: the person's copy is only ever fast-forwarded, and anything else is left alone with a reason. Each row is a real clone
// of a real remote, because the answer depends on what git says.
func TestSync(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, local, other string) // other is a second clone that pushes to the server
		want  string
		moved bool // the local file changed
	}{
		{"nothing new", func(t *testing.T, l, o string) {}, SyncCurrent, false},
		{"new commits on the server are taken", func(t *testing.T, l, o string) {
			write(t, o, "f.txt", "server")
			sh(t, o, "commit", "-aq", "-m", "s")
			sh(t, o, "push", "-q", "origin", "main")
		}, SyncUpdated, true},
		{"unsaved edits are never touched", func(t *testing.T, l, o string) {
			write(t, o, "f.txt", "server")
			sh(t, o, "commit", "-aq", "-m", "s")
			sh(t, o, "push", "-q", "origin", "main")
			write(t, l, "f.txt", "mine")
		}, SyncDirty, false},
		{"another branch is not switched", func(t *testing.T, l, o string) {
			write(t, o, "f.txt", "server")
			sh(t, o, "commit", "-aq", "-m", "s")
			sh(t, o, "push", "-q", "origin", "main")
			sh(t, l, "checkout", "-q", "-b", "spec/x")
		}, SyncOtherBr, false},
		{"a local commit on the base is not merged or rewritten", func(t *testing.T, l, o string) {
			write(t, o, "f.txt", "server")
			sh(t, o, "commit", "-aq", "-m", "s")
			sh(t, o, "push", "-q", "origin", "main")
			write(t, l, "g.txt", "mine")
			sh(t, l, "add", "g.txt")
			sh(t, l, "commit", "-q", "-m", "local")
		}, SyncDiverged, false},
		{"a server that cannot be reached leaves the copy as it is", func(t *testing.T, l, o string) {
			sh(t, l, "remote", "set-url", "origin", filepath.Join(l, "no-such-remote"))
		}, SyncOffline, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			server, local, other := filepath.Join(tmp, "server.git"), filepath.Join(tmp, "local"), filepath.Join(tmp, "other")
			sh(t, tmp, "init", "-q", "--bare", server)
			sh(t, tmp, "clone", "-q", server, local)
			write(t, local, "f.txt", "base")
			sh(t, local, "add", ".")
			sh(t, local, "commit", "-q", "-m", "init")
			sh(t, local, "push", "-q", "origin", "HEAD:main")
			sh(t, local, "branch", "-M", "main")
			sh(t, local, "branch", "-q", "--set-upstream-to=origin/main", "main")
			sh(t, tmp, "clone", "-q", server, other)
			c.setup(t, local, other)
			before, _ := os.ReadFile(filepath.Join(local, "f.txt"))
			got := Sync(context.Background(), local)
			if got.State != c.want {
				t.Fatalf("state = %q (%s), want %q", got.State, got.Message, c.want)
			}
			after, _ := os.ReadFile(filepath.Join(local, "f.txt"))
			if moved := string(before) != string(after); moved != c.moved {
				t.Errorf("file changed = %v, want %v (%q -> %q)", moved, c.moved, before, after)
			}
			if got.Message == "" {
				t.Error("no message for the person")
			}
		})
	}
}
