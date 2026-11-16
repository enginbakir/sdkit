// Package worktree manages the isolated git worktrees of parallel tasks:
// ../<repo>-wt/<task-id> on branch wt/<current-branch>/<task-id>.
package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
)

// Tree is the worktree context of the branch the tasks belong to.
type Tree struct {
	Top, Cur, Root string // repo top level, current branch, directory holding task worktrees
}

// Open reads the context from dir; a detached HEAD is refused.
func Open(dir string) (Tree, error) {
	top, err := gitx.Top(dir)
	if err != nil {
		return Tree{}, errors.New("not in a git repository")
	}
	cur, err := gitx.Run(top, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || cur == "HEAD" {
		return Tree{}, errors.New("detached HEAD: check out the task's branch first")
	}
	return Tree{top, cur, filepath.Join(filepath.Dir(top), filepath.Base(top)+"-wt")}, nil
}

// Branch is the branch of task id.
func (t Tree) Branch(id string) string { return "wt/" + t.Cur + "/" + id }

// Path is the worktree directory of task id.
func (t Tree) Path(id string) string { return filepath.Join(t.Root, id) }

// ValidID: letters, digits, _ . - only.
func ValidID(id string) bool {
	return id != "" && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-')
	}) < 0
}

// Add creates the worktree of task id from HEAD and returns its path.
func (t Tree) Add(id string) (string, error) {
	if out, _ := gitx.Run(t.Top, "status", "--porcelain", "--untracked-files=no"); out != "" {
		return "", errors.New("commit or stash first: the worktree starts from HEAD")
	}
	if err := os.MkdirAll(t.Root, 0o755); err != nil {
		return "", err
	}
	if _, err := gitx.Run(t.Top, "worktree", "add", "-q", "-b", t.Branch(id), t.Path(id), "HEAD"); err != nil {
		return "", err
	}
	return t.Path(id), nil
}

// Existing returns the path of task id's worktree, or an error when there is none.
func (t Tree) Existing(id string) (string, error) {
	if st, err := os.Stat(t.Path(id)); id == "" || err != nil || !st.IsDir() {
		return "", fmt.Errorf("no worktree for task '%s' (worktree add first)", id)
	}
	return t.Path(id), nil
}

// Remove deletes the worktree and the branch of task id (the branch only when merged unless force).
func (t Tree) Remove(id string, force bool) error {
	if _, err := t.Existing(id); err != nil {
		return err
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	if _, err := gitx.Run(t.Top, append(args, t.Path(id))...); err != nil {
		return err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitx.Run(t.Top, "branch", "-q", flag, t.Branch(id))
	return err
}

// Merge merges the task branch into the current branch (no fast-forward); a conflict is aborted.
func (t Tree) Merge(id string) error { return t.MergeChecked(id, nil) }

// MergeChecked is Merge, but first hands the files the task branch changed since it forked
// from HEAD to check; a non-nil error from check refuses the merge.
func (t Tree) MergeChecked(id string, check func(changed []string) error) error {
	if p, err := t.Existing(id); err == nil {
		if out, _ := gitx.Run(p, "status", "--porcelain"); out != "" {
			return fmt.Errorf("task %s has uncommitted changes; commit them in its worktree first", id)
		}
	}
	if check != nil {
		base, err := gitx.MergeBase(t.Top, "HEAD", t.Branch(id))
		if err != nil {
			return fmt.Errorf("no branch for task %s (worktree add first)", id)
		}
		changed, err := gitx.ChangedFiles(t.Top, base+".."+t.Branch(id), false)
		if err != nil {
			return err
		}
		if err := check(changed); err != nil {
			return err
		}
	}
	if _, err := gitx.Run(t.Top, "merge", "--no-ff", "-m", "merge task "+id, t.Branch(id)); err != nil {
		_, _ = gitx.Run(t.Top, "merge", "--abort")
		return fmt.Errorf("conflict merging %s: the write sets overlapped; fix the plan (references/parallel.md)", id)
	}
	return nil
}

// Clean removes the worktrees whose branch is already merged into HEAD and returns their task ids.
func (t Tree) Clean() []string {
	var done []string
	out, _ := gitx.Run(t.Top, "for-each-ref", "--format=%(refname:short)", "refs/heads/wt/"+t.Cur+"/")
	for _, b := range gitx.Lines(out) {
		id := strings.TrimPrefix(b, "wt/"+t.Cur+"/")
		if _, err := gitx.Run(t.Top, "merge-base", "--is-ancestor", b, "HEAD"); err != nil {
			continue
		}
		_, _ = gitx.Run(t.Top, "worktree", "remove", "--force", t.Path(id))
		if _, err := gitx.Run(t.Top, "branch", "-q", "-d", b); err == nil {
			done = append(done, id)
		}
	}
	return done
}

// List returns the `git worktree list` lines that live under Root.
func (t Tree) List() []string {
	out, _ := gitx.Run(t.Top, "worktree", "list")
	var l []string
	for _, ln := range gitx.Lines(out) {
		if strings.Contains(filepath.ToSlash(ln), filepath.ToSlash(t.Root)) {
			l = append(l, ln)
		}
	}
	return l
}
