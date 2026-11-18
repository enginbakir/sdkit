package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a main tree on branch main with a remote, the files git ignores that a session needs, and the base commit
func mainWithRemote(t *testing.T) string {
	root := scratchRepo(t, "/prespec/\n/.sdkit/work/\n/.sdkit/sdkit.local.env\n/.claude/settings.local.json\n/.claude/skills/\n")
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare")
	os.MkdirAll(filepath.Join(root, "prespec"), 0o755)
	os.MkdirAll(filepath.Join(root, ".claude", "skills", "generate-specs"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude", "CLAUDE.md"), []byte("kit block\n"), 0o644) // tracked, so .claude/ exists in a checkout
	os.WriteFile(filepath.Join(root, ".claude", "skills", "generate-specs", "SKILL.md"), []byte("skill\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".claude", "settings.local.json"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".sdkit", "sdkit.local.env"), []byte("BITBUCKET_TOKEN=secret\n"), 0o600)
	os.WriteFile(filepath.Join(root, "prespec", "odeme.md"), []byte("# ön-spec\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".sdkit", "work", "spec-draft", "baska"), 0o755)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "base")
	gitIn(t, root, "remote", "add", "origin", remote)
	gitIn(t, root, "push", "-q", "origin", "HEAD:main")
	gitIn(t, root, "fetch", "-q", "origin")
	return root
}

func TestProvisionMakesAWorktreePerSpec(t *testing.T) {
	root := mainWithRemote(t)
	p, created, err := Provision(root, "odeme")
	if err != nil || !created || !strings.HasSuffix(filepath.ToSlash(p), "-wt/spec/odeme") {
		t.Fatalf("path %q created %v err %v", p, created, err)
	}
	if b := Branch(p); b != "spec/odeme" {
		t.Errorf("the worktree is on the spec's own branch: %s", b)
	}
	if b := Branch(root); b == "spec/odeme" {
		t.Error("the main tree stays where it was")
	}
	for _, rel := range []string{".claude/settings.local.json", ".sdkit/sdkit.local.env", ".claude/skills/generate-specs/SKILL.md", "prespec/odeme.md", ".claude/CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(p, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the session needs %s in its tree: %v", rel, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(p, ".sdkit", "work", "spec-draft", "baska")); err == nil {
		t.Error("the work directory is the spec's own: another spec's drafts must not appear in it")
	}
	if got, _ := os.ReadFile(filepath.Join(p, ".sdkit", "sdkit.local.env")); !strings.Contains(string(got), "secret") {
		t.Error("the tokens are reachable from the worktree")
	}
	if p2, created, err := Provision(root, "odeme"); err != nil || created || p2 != p {
		t.Errorf("second call reuses it: %q %v %v", p2, created, err)
	}
	if RootFor(root, "odeme") != p || RootFor(root, "baska") != root {
		t.Errorf("RootFor: %q %q", RootFor(root, "odeme"), RootFor(root, "baska"))
	}
}

func TestProvisionKeepsASpecThatLivesInTheMainTree(t *testing.T) {
	root := mainWithRemote(t)
	// "baska" has a draft in the main tree's work directory: that work is not in a fresh checkout
	if p, created, err := Provision(root, "baska"); err != nil || created || p != root {
		t.Errorf("a spec with a draft in the main tree stays there: %q %v %v", p, created, err)
	}
	gitIn(t, root, "checkout", "-q", "-b", "spec/yeni")
	if p, created, _ := Provision(root, "yeni"); created || p != root {
		t.Errorf("a spec whose branch the main tree has checked out stays there: %q %v", p, created)
	}
	gitIn(t, root, "checkout", "-q", "-b", "spec/uzun--degisiklik")
	if p, created, _ := Provision(root, "uzun"); created || p != root {
		t.Errorf("so does the spec whose change branch it is on: %q %v", p, created)
	}
}

func TestListSpecsReadsASpecFromItsWorktree(t *testing.T) {
	root := mainWithRemote(t)
	p, _, err := Provision(root, "odeme")
	if err != nil {
		t.Fatal(err)
	}
	state := func() string {
		for _, s := range ListSpecs(root) {
			if s.ID == "odeme" {
				return s.State
			}
		}
		return ""
	}
	if state() != "new" {
		t.Fatalf("a pre-spec and nothing else: %s", state())
	}
	os.MkdirAll(filepath.Join(p, ".sdkit", "work", "spec-draft", "odeme"), 0o755) // the agent drafted it in its worktree
	if state() != "draft" {
		t.Errorf("the draft is in the worktree, not the main tree, and the list must see it: %s", state())
	}
}

func TestRemoveWorktreeRefusesUnsavedWork(t *testing.T) {
	root := mainWithRemote(t)
	p, _, _ := Provision(root, "odeme")
	os.WriteFile(filepath.Join(p, "scratch.md"), []byte("x"), 0o644)
	if err := RemoveWorktree(root, "odeme"); err == nil {
		t.Error("untracked work in the worktree must not be thrown away")
	}
	os.Remove(filepath.Join(p, "scratch.md"))
	if err := RemoveWorktree(root, "odeme"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("the directory is gone")
	}
	if err := RemoveWorktree(root, "odeme"); err == nil {
		t.Error("nothing left to remove")
	}
}
