// Package guard is the host-independent guard of the git hooks (`sdkit guard pre-commit|pre-push`):
// the same rules as the Claude Code hooks, enforced at commit and push time so they hold for any agent or human.
// Overrides, per command: ALLOW_PROTECTED=1, ALLOW_PROTECTED_PUSH=1, GUARD_SKIP_VERIFY=1, SDD_SKIP=1.
package guard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/glob"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/verify"
)

// SpecChecker is the spec merge gate (c): Check reports whether spec id is mergeable. P3-09 supplies it.
type SpecChecker interface {
	Check(root, id string) (report string, err error)
}

// Specs is nil until P3-09: the check is then unavailable and never blocks.
var Specs SpecChecker

var foldCase = runtime.GOOS == "windows"

type run struct {
	root string
	e    *sdd.Env
	out  io.Writer
	fail bool
}

func newRun(root string, out io.Writer) *run {
	e := sdd.New(root)
	e.Out, e.Err = io.Discard, io.Discard
	return &run{root: root, e: e, out: out}
}

func (r *run) bad(f string, a ...any) { fmt.Fprintf(r.out, "guard: "+f+"\n", a...); r.fail = true }

// gate asks the phase gate: strict blocks, guided warns (a git hook cannot ask), off skips. SDD_SKIP=1 bypasses once.
func (r *run) gate(kind, p string) {
	if r.e.Get("SDD_SKIP") == "1" || r.e.Mode() == "off" {
		return
	}
	ok, why := r.e.Gate(kind, p)
	switch {
	case ok:
	case r.e.Mode() == "strict":
		r.bad("%s (SDD_SKIP=1 bypasses once)", why)
	default:
		fmt.Fprintf(r.out, "guard (warning): %s\n", why)
	}
}

func code(r *run) int {
	if r.fail {
		return 1
	}
	return 0
}

// PreCommit checks every staged file: the edit gate, SECRET_GLOBS, .frozen and PROTECTED_PATHS. Messages go to out.
func PreCommit(root string, out io.Writer) int {
	r := newRun(root, out)
	staged, _ := gitx.Run(root, "diff", "--cached", "--name-only", "--diff-filter=ACMRD") // a deletion is a code change too
	frozen, _ := os.ReadFile(filepath.Join(r.e.P.Dir(), ".frozen"))
	kit := filepath.ToSlash(r.e.P.KitDir)
	for _, f := range gitx.Lines(staged) {
		r.gate("edit", f)
		if glob.MatchList(f, r.e.Get("SECRET_GLOBS"), foldCase) {
			r.bad("'%s' matches SECRET_GLOBS; secrets must not be committed.", f)
		}
		for _, l := range strings.Split(string(frozen), "\n") {
			if f == strings.TrimSuffix(l, "\r") {
				r.bad("'%s' is a frozen test (%s/.frozen); fix the implementation, not the test.", f, kit)
				break
			}
		}
		if pp := r.e.Get("PROTECTED_PATHS"); pp != "" && r.e.Get("ALLOW_PROTECTED") != "1" {
			for _, p := range strings.Split(pp, ",") {
				if p != "" && glob.HasPrefix(f, p, foldCase) {
					r.bad("'%s' is under protected path '%s' (see %s/project.md). Re-run with ALLOW_PROTECTED=1 once you have approval.", f, p, kit)
				}
			}
		}
	}
	return code(r)
}

type ref struct{ lsha, rref, rsha string }

func readRefs(in io.Reader) (refs []ref) {
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) >= 4 {
			refs = append(refs, ref{f[1], f[2], f[3]})
		}
	}
	return refs
}

// PrePush checks the push gate, PROTECTED_BRANCHES, the spec merge gate (SPEC_CHECK_ON_PUSH=1) and runs verify --fast --since-head.
// refs is the git pre-push stdin: <local ref> <local sha> <remote ref> <remote sha> per pushed ref.
func PrePush(root string, refsIn io.Reader, out io.Writer) int {
	r := newRun(root, out)
	r.gate("push", "")
	refs := readRefs(refsIn)
	if pb := r.e.Get("PROTECTED_BRANCHES"); pb != "" && r.e.Get("ALLOW_PROTECTED_PUSH") != "1" {
		for _, f := range refs {
			if b := strings.TrimPrefix(f.rref, "refs/heads/"); glob.MatchList(b, pb, foldCase) {
				r.bad("push to protected branch '%s' needs approval. Re-run with ALLOW_PROTECTED_PUSH=1.", b)
			}
		}
	}
	if r.e.Get("SPEC_CHECK_ON_PUSH") == "1" && Specs != nil {
		r.specs(refs)
	}
	if !r.fail && r.e.Get("GUARD_SKIP_VERIFY") != "1" {
		r.verify()
	}
	return code(r)
}

// specs is gate (c): pushing a spec's BASE branch (spec/<id>) or the base branch itself checks each spec whose meta changed.
// A change branch (spec/<id>--<slug>) is never checked: that is how a change goes to review. Only the checked-out commit can be checked.
func (r *run) specs(refs []ref) {
	sb, dir := r.e.Get("BASE_BRANCH"), r.e.Get("SPECS_DIR")
	if sb == "" {
		sb = "main"
	}
	if dir == "" {
		dir = "specs"
	}
	head := gitx.RevParse(r.root, "HEAD")
	meta := regexp.MustCompile("^" + regexp.QuoteMeta(dir) + `/([^/]+)/spec\.meta\.json$`)
	for _, f := range refs {
		if f.lsha == "" || f.lsha != head {
			continue
		}
		b, ids := strings.TrimPrefix(f.rref, "refs/heads/"), []string(nil)
		switch {
		case strings.HasPrefix(b, "spec/") && strings.Contains(b, "--"):
			continue
		case strings.HasPrefix(b, "spec/"):
			ids = []string{strings.TrimPrefix(b, "spec/")}
		case b == sb:
			from := f.rsha
			if strings.Trim(from, "0") == "" {
				from, _ = gitx.MergeBase(r.root, "origin/"+sb, f.lsha)
			}
			if from == "" {
				continue
			}
			d, _ := gitx.Run(r.root, "diff", "--name-only", from, f.lsha, "--", dir)
			seen := map[string]bool{}
			for _, l := range gitx.Lines(d) {
				if m := meta.FindStringSubmatch(l); m != nil && !seen[m[1]] {
					seen[m[1]] = true
					ids = append(ids, m[1])
				}
			}
		default:
			continue
		}
		for _, id := range ids {
			if rep, err := Specs.Check(r.root, id); err != nil {
				fmt.Fprintln(r.out, rep)
				r.bad("spec '%s' is not mergeable yet (spec check). Fix it, or unset SPEC_CHECK_ON_PUSH for this push.", id)
			}
		}
	}
}

// verify runs the fast, since-HEAD verification (VERIFY_CMD, a project executable, when set) and shows the last 30 lines of a red run.
func (r *run) verify() {
	var buf bytes.Buffer
	ok := true
	if v := r.e.Get("VERIFY_CMD"); v != "" {
		p := filepath.Join(r.root, v)
		if _, err := os.Stat(p); err != nil {
			return
		}
		c := exec.CommandContext(context.Background(), p, "--fast", "--since-head")
		c.Dir, c.Stdout, c.Stderr = r.root, &buf, &buf
		ok = c.Run() == nil
	} else {
		ok = verify.Run(context.Background(), r.root, verify.Options{Fast: true, SinceHead: true}, &buf) == 0
	}
	if !ok {
		l := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(l) > 30 {
			l = l[len(l)-30:]
		}
		fmt.Fprintln(r.out, strings.Join(l, "\n"))
		r.bad("verify (vet/build) is red; fix it before pushing (GUARD_SKIP_VERIFY=1 to bypass deliberately).")
	}
}
