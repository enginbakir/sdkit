package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchRepo is a git repo with a project directory; ignore says what .gitignore holds.
func scratchRepo(t *testing.T, ignore string) string {
	t.Helper()
	root := t.TempDir()
	c := exec.Command("git", "init", "-q")
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	os.MkdirAll(filepath.Join(root, ".sdkit"), 0o755)
	os.WriteFile(filepath.Join(root, ".sdkit", "sdkit.env"), []byte("SDKIT_ENV=analyst\nPRESPEC_DIR=prespec\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(ignore), 0o644)
	return root
}

func TestWritePrespec(t *testing.T) {
	root := scratchRepo(t, "/prespec/\n")
	cases := []struct {
		name, id, text string
		overwrite      bool
		wantErr        string
	}{
		{"new pre-spec", "odeme-akisi", "metin", false, ""},
		{"same id again is refused", "odeme-akisi", "baska", false, "zaten var"},
		{"same id replaced on request", "odeme-akisi", "baska", true, ""},
		{"empty text", "bos", "  \n", false, "boş"},
		{"id that would escape the directory", "../x", "m", false, "Ad harf"},
		{"id with a space", "iki kelime", "m", false, "Ad harf"},
		{"id with a dot would break the branch name", "a.b", "m", false, "Ad harf"},
		{"too long text", "uzun", strings.Repeat("x", maxPrespec+1), false, "uzun"},
	}
	for _, c := range cases {
		err := WritePrespec(root, c.id, c.text, c.overwrite)
		if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "prespec", "odeme-akisi.md")); string(b) != "baska" {
		t.Errorf("content = %q", b)
	}
}

func TestWritePrespecNeverWritesWhereGitWouldCommit(t *testing.T) {
	root := scratchRepo(t, "")
	if err := WritePrespec(root, "x", "metin", false); err == nil {
		t.Fatal("a pre-spec in a directory git does not ignore must be refused")
	}
	if _, err := os.Stat(filepath.Join(root, "prespec", "x.md")); err == nil {
		t.Error("the refusal must not leave the file behind")
	}
}

func TestTask(t *testing.T) {
	if got, err := Task("", "generate-specs", "odeme-akisi", TaskInput{}); err != nil || !strings.Contains(got, `"odeme-akisi"`) || !strings.Contains(got, "generate-specs") {
		t.Errorf("task = %q, %v", got, err)
	}
	if _, err := Task("", "generate-specs", "FORGOT.PASSWORD-v2", TaskInput{}); err != nil {
		t.Errorf("an id that already exists in a project (it has a dot) must be continuable: %v", err)
	}
	for _, a := range []string{"update-specs", "merge-specs"} {
		if got, err := Task("", a, "odeme-akisi", TaskInput{}); err != nil || !strings.Contains(got, a) {
			t.Errorf("%s: %q, %v", a, got, err)
		}
	}
	for _, c := range [][2]string{{"rm -rf", "x"}, {"generate-specs", "x; rm -rf /"}, {"generate-specs", ""}, {"generate-specs", "a b"}, {"", "x"}} {
		if _, err := Task("", c[0], c[1], TaskInput{}); err == nil {
			t.Errorf("Task(%q, %q) must be refused: the page names an action, it does not write instructions", c[0], c[1])
		}
	}
}

func TestListSpecs(t *testing.T) {
	root := scratchRepo(t, "/prespec/\n")
	WritePrespec(root, "yeni", "metin", false)
	os.MkdirAll(filepath.Join(root, "specs", "eski"), 0o755)
	var got []string
	for _, s := range ListSpecs(root) {
		got = append(got, s.ID+":"+s.State+":"+map[bool]string{true: "devam", false: "-"}[s.Continue])
	}
	if strings.Join(got, " ") != "eski:output:devam yeni:new:devam" {
		t.Errorf("list = %v", got)
	}
}

func TestSoftProblemsDoNotBlock(t *testing.T) {
	items := ParseDoctor("  FAIL  project.md still has TODO placeholders\n  ok    git repository")
	if !Ready(items) {
		t.Error("an unfilled project.md is the Tech Lead's to fix and must not keep the analyst out")
	}
	var shown bool
	for _, it := range items {
		shown = shown || (it.Main && it.Soft)
	}
	if !shown {
		t.Error("it must still be shown")
	}
	if Ready(ParseDoctor("  FAIL  openspec CLI not found")) {
		t.Error("a missing OpenSpec stops the work")
	}
}
