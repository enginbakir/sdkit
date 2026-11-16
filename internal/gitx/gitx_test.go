package gitx

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTop(t *testing.T) {
	d := t.TempDir()
	if err := exec.Command("git", "-C", d, "init", "-q").Run(); err != nil {
		t.Skip(err)
	}
	got, err := Top(d)
	if want, _ := filepath.EvalSymlinks(d); err != nil || filepath.Clean(got) != want && got != d {
		t.Fatalf("got %q err %v", got, err)
	}
}
