package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/spec"
)

// A spec worked on in the panel lives in its own git worktree, ../<repo>-wt/spec/<id>, on its own branch spec/<id>: two specs can then
// advance at the same time without one switching the branch under the other. What git does not carry into a new checkout (the host's
// skills and hooks, the personal settings with the tokens, the pre-specs) is linked from the main tree; the work directory is not,
// so each spec has its own drafts, review evidence and token ledger.

// WorktreePath is where the worktree of spec id lives.
func WorktreePath(root, id string) string {
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-wt", "spec", id)
}

// Worktrees maps spec id to the path of its worktree, for the ones that exist.
func Worktrees(root string) map[string]string {
	out := map[string]string{}
	list, err := gitx.Run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return out
	}
	// git prints real paths; the main tree may have been named through a link (macOS /var -> /private/var)
	real := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		real = r
	}
	prefix := filepath.ToSlash(filepath.Join(filepath.Dir(real), filepath.Base(real)+"-wt", "spec")) + "/"
	for _, line := range gitx.Lines(list) {
		p, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if id, ok := strings.CutPrefix(filepath.ToSlash(p), prefix); ok && ValidID(id) || ok && reKitID.MatchString(id) {
			out[id] = filepath.FromSlash(p)
		}
	}
	return out
}

// RootFor is the tree a spec's files live in: its worktree when it has one, else the main tree.
func RootFor(root, id string) string {
	if p, ok := Worktrees(root)[id]; ok {
		return p
	}
	return root
}

// mainHoldsSpec says whether the main tree carries work on the spec that a new worktree would not have: a draft in its work directory,
// files of it git does not know yet, or the spec's own branch checked out. Such a spec keeps working in the main tree.
func mainHoldsSpec(root, id string) bool {
	_, pj, _ := project.Load(root)
	if _, err := os.Stat(filepath.Join(pj.Dir(), "work", "spec-draft", id)); err == nil {
		return true
	}
	if b := Branch(root); b == "spec/"+id || strings.HasPrefix(b, "spec/"+id+"--") {
		return true
	}
	st, _ := gitx.Run(root, "status", "--porcelain", "--", "specs/"+id)
	return strings.TrimSpace(st) != ""
}

// Provision returns the tree a run of the spec should work in, making the worktree when the spec starts fresh. created says whether it
// was just made.
func Provision(root, id string) (path string, created bool, err error) {
	if p, ok := Worktrees(root)[id]; ok {
		return p, false, nil
	}
	if mainHoldsSpec(root, id) {
		return root, false, nil
	}
	s, _, _ := project.Load(root)
	base := s.String("BASE_BRANCH")
	if base == "" || strings.HasPrefix(base, "__") {
		base = "main"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	proc.Run(ctx, root, []string{"GIT_TERMINAL_PROMPT=0"}, "git", "fetch", "-q", "origin")
	cancel()
	path = WorktreePath(root, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	branch := "spec/" + id
	has := func(ref string) bool { _, e := gitx.Run(root, "show-ref", "--verify", "-q", ref); return e == nil }
	var args []string
	switch {
	case has("refs/heads/" + branch):
		args = []string{"worktree", "add", "-q", path, branch}
	case has("refs/remotes/origin/" + branch):
		args = []string{"worktree", "add", "-q", "--track", "-b", branch, path, "origin/" + branch}
	case has("refs/remotes/origin/" + base):
		args = []string{"worktree", "add", "-q", "-b", branch, path, "origin/" + base}
	default:
		args = []string{"worktree", "add", "-q", "-b", branch, path}
	}
	if _, err := gitx.Run(root, args...); err != nil {
		return "", false, fmt.Errorf("çalışma alanı hazırlanamadı: %v", err)
	}
	if err := linkLocal(root, path, filepath.Join(pj(root).KitDir, "work")); err != nil {
		return "", false, err
	}
	if p, ok := Worktrees(root)[id]; ok {
		path = p // as git names it, so later calls agree
	}
	return path, true, nil
}

func pj(root string) project.Project { _, p, _ := project.Load(root); return p }

// linkLocal brings into the worktree what git leaves out of a checkout and the session needs: every ignored file or directory of the
// main tree except the work directory and caches (a spec's drafts, review evidence and ledger stay its own). A file is linked, so the
// tokens saved on the Configuration page reach every worktree at once. A directory is copied, once: git's `dir/` ignore rules do not
// match a link to a directory, which would show up as untracked work (and the copy of a pre-spec is that spec's own to edit). After an
// `sdkit upgrade` a worktree keeps its old skill copies until it is removed and made again. Where links cannot be made (Windows
// without the privilege) files are copied too.
func linkLocal(main, wt, skipWork string) error {
	out, err := gitx.Run(main, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return err
	}
	for _, rel := range gitx.Lines(out) {
		rel = strings.TrimSuffix(filepath.ToSlash(rel), "/")
		if rel == "" || rel == filepath.ToSlash(skipWork) || strings.HasPrefix(rel, filepath.ToSlash(skipWork)+"/") || strings.HasSuffix(rel, ".verify-cache") || strings.HasPrefix(rel, ".git") {
			continue
		}
		src, dst := filepath.Join(main, filepath.FromSlash(rel)), filepath.Join(wt, filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if st, err := os.Stat(src); err == nil && st.IsDir() {
			if err := copyAny(src, dst); err != nil {
				return err
			}
			continue
		}
		if err := os.Symlink(src, dst); err != nil {
			if runtime.GOOS != "windows" {
				return err
			}
			if err := copyAny(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyAny(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyAny(p, filepath.Join(dst, rel))
	})
}

// RemoveWorktree deletes a spec's worktree (never with force: unsaved work or a held branch refuses) and leaves its branch.
func RemoveWorktree(root, id string) error {
	p, ok := Worktrees(root)[id]
	if !ok {
		return errors.New("bu spec için ayrı bir çalışma alanı yok")
	}
	if st, _ := gitx.Run(p, "status", "--porcelain"); strings.TrimSpace(st) != "" {
		return errors.New("çalışma alanında kaydedilmemiş değişiklikler var; silinmedi")
	}
	if _, err := gitx.Run(root, "worktree", "remove", p); err != nil {
		return fmt.Errorf("çalışma alanı silinemedi: %v", err)
	}
	return nil
}

// ListSpecs is every spec of the project with where it stands. A spec with a worktree of its own is read from there: its drafts and
// review evidence are not in the main tree.
func ListSpecs(root string) []SpecItem {
	items := listLocal(root)
	wts := Worktrees(root)
	if len(wts) == 0 {
		return items
	}
	seen := map[string]int{}
	for i, it := range items {
		seen[it.ID] = i
	}
	for id, p := range wts {
		for _, e := range spec.List(p) {
			if e.ID != id {
				continue
			}
			it := specItem(e)
			if i, ok := seen[id]; ok {
				items[i] = it
			} else {
				items = append(items, it)
			}
		}
	}
	sortItems(items)
	return items
}
