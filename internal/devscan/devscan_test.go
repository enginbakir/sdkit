package devscan

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.txt"), []byte("http://u:pw@h.io\n"+"AKIA"+"ABCDEFGHIJKLMNOP\n"), 0o644)
	exec.Command("git", "-C", d, "init", "-q").Run()
	exec.Command("git", "-C", d, "add", "a.txt").Run()
	os.MkdirAll(filepath.Join(d, "packaging"), 0o755)
	os.WriteFile(filepath.Join(d, "packaging", "scan-allow.txt"), []byte("u:pw\n"), 0o644)
	if Run(d, false, io.Discard) != 1 {
		t.Fatal("key must be found")
	}
	os.WriteFile(filepath.Join(d, "a.txt"), []byte("http://u:pw@h.io\n"), 0o644)
	if Run(d, false, io.Discard) != 0 {
		t.Fatal("allow-listed url must pass")
	}
}
