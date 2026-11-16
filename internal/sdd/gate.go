package sdd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
)

// RelPath is p relative to root with //, . and .. resolved and symlinks followed where they exist, in slash form.
// A path outside root is returned absolute; the root itself is "". Gates match globs against this, so docs/../src/x.go
// is src/x.go and .sdkit/./.sdd/state.json is .sdkit/.sdd/state.json.
func RelPath(root, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	for i := 0; ; {
		p = filepath.Clean(p) // lexical first, as the bash did: a symlink followed by .. is judged on the clean path
		p = filepath.Join(resolveDir(filepath.Dir(p)), filepath.Base(p))
		i++
		fi, err := os.Lstat(p)
		if i >= 8 || err != nil || fi.Mode()&os.ModeSymlink == 0 {
			break
		}
		t, err := os.Readlink(p)
		if err != nil {
			break
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(p), t)
		}
		p = t
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	root = filepath.Clean(root)
	if p == root {
		return ""
	}
	pre := root + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		if len(p) > len(pre) && strings.EqualFold(p[:len(pre)], pre) {
			return filepath.ToSlash(p[len(pre):])
		}
	} else if strings.HasPrefix(p, pre) {
		return filepath.ToSlash(p[len(pre):])
	}
	return filepath.ToSlash(p)
}

// resolveDir follows the symlinks of the longest existing ancestor of d (a directory that does not exist yet, such as
// .sdkit/attestations, must still be judged by where its parent really is).
func resolveDir(d string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			return filepath.Join(r, rest)
		}
		up := filepath.Dir(d)
		if up == d {
			return filepath.Join(d, rest)
		}
		rest, d = filepath.Join(filepath.Base(d), rest), up
	}
}

const defaultTestGlobs = "*_test.go,*.test.ts,*.test.tsx,*.test.js,*.spec.ts,*.spec.js,test_*.py,*_test.py,*Test.java,*Tests.java,*IT.java,*Test.kt,*Tests.kt,tests/*,test/*,*/tests/*,*/test/*,*/__tests__/*,src/test/*"

// freeGlobs are the paths no task is needed for: the project directory, specs, docs and markdown.
func (e *Env) freeGlobs() string {
	return e.kitPath() + "/*,openspec/*,docs/*,*.md," + e.Get("SDD_FREE_GLOBS")
}

// Gate is the single decision point for "may this happen in the current phase?". Hooks, git hooks and CI all ask here, so every
// host enforces the same rule. kind is "edit" (path is the file) or "push".
//
//	edit  no task / intake -> docs, specs and project files only;  proposed -> test files only;  red and later -> anything
//	push  needs phase verified or pr, and the verified diff must still be the current one
//
// It returns whether the move is allowed and, when not, the reason. A refusal is reported as phase.reject.
func (e *Env) Gate(kind, p string) (bool, string) {
	if e.Mode() == "off" {
		return true, ""
	}
	t := e.load()
	phase := ""
	if t != nil {
		phase = t.Phase
	}
	ok, why := e.gate(kind, p, t, phase)
	if !ok {
		e.emit("phase.reject", "phase", phase, "category", "gate", "reason", "out-of-phase", "source", "gate-"+kind)
	}
	return ok, why
}

func blockedBy(t *Task) bool { return t != nil && t.Blocked != nil && t.Blocked.Kind != "" }

func (e *Env) gate(kind, p string, t *Task, phase string) (bool, string) {
	switch kind {
	case "edit":
		if p == "" {
			return true, ""
		}
		rel := RelPath(e.Root, p)
		if rel == "" {
			return true, ""
		}
		kd := e.kitPath()
		if strings.HasPrefix(rel, kd+"/.sdd/") || strings.HasPrefix(rel, kd+"/attestations/") {
			return false, "The workflow state and attestations are written only by sdkit sdd and the verifier hook."
		}
		if e.managed() {
			if why := e.Mgd.Edit(e, rel); why != "" {
				return false, why
			}
		}
		if glob.MatchList(rel, e.freeGlobs(), false) {
			return true, ""
		}
		if blockedBy(t) {
			return false, "Task '" + t.Task + "' is blocked by a " + t.Blocked.Kind + " request (" + t.Blocked.Ref + "): '" + rel + "' may not change until sdkit sdd revalidate --sha <new approved revision>."
		}
		switch phase {
		case "":
			return false, "No spec-driven task is open, so '" + rel + "' may not change yet. Start one with the spec-driven-dev skill (it opens the task after you confirm the risk label)."
		case "intake":
			return false, "Task '" + t.Task + "' is in 'intake': write the proposal with a Definition of Done and run 'sdkit sdd advance proposed' before changing '" + rel + "'."
		case "proposed":
			if glob.MatchList(rel, e.setting("SDD_TEST_GLOBS", defaultTestGlobs), false) {
				return true, ""
			}
			return false, "Phase 'proposed' is test-first: only test files may change. Write the failing test, then run 'sdkit sdd advance red --test-cmd <cmd>'. '" + rel + "' is not a test file (SDD_TEST_GLOBS)."
		}
		return true, ""
	case "push":
		if t != nil && t.Managed && !blockedBy(t) && e.Mgd != nil {
			if why := e.Mgd.Push(e, t); why != "" {
				return false, why
			}
		}
		if blockedBy(t) {
			return false, "Task '" + t.Task + "' is blocked by a " + t.Blocked.Kind + " request (" + t.Blocked.Ref + "): no push or PR until sdkit sdd revalidate --sha <new approved revision>."
		}
		switch phase {
		case "":
			return false, "Pushing or opening a PR outside a spec-driven task. Start one with the spec-driven-dev skill."
		case "verified", "pr":
			if !e.checkFresh(t) {
				return false, "The diff changed after verification. Re-run the verifier, then 'sdkit sdd advance verified'."
			}
			return true, ""
		}
		return false, "Task is in phase '" + phase + "'; push and PR come after 'verified' (tests green, then verification)."
	}
	return false, "usage: gate edit <path> | gate push"
}
