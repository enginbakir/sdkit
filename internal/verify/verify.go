// Package verify is the verify engine: it computes what changed against the diff base,
// runs the stack's steps and the project's verify.d policies, and caches a green result
// per exact code state.
package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/stack"
)

// Options are the flags of `sdkit verify`.
type Options struct{ Fast, SinceHead, NoCache bool }

// ResolveBase returns the ref to diff against: the same rules as `sdkit base`
// (1.x verify-core.sh called resolve-base.sh). A variable so tests can pin it.
var ResolveBase = func(root string, s *envfile.Settings) string {
	return base.Resolve(root, base.Getter(s.Raw))
}

// Parse reads the flags; unknown ones are ignored as in 1.x.
func Parse(args []string) Options {
	var o Options
	for _, a := range args {
		switch a {
		case "--fast":
			o.Fast = true
		case "--since-head":
			o.SinceHead = true
		case "--no-cache":
			o.NoCache = true
		}
	}
	return o
}

type reporter struct {
	buf          bytes.Buffer
	fails, warns int
}

func (r *reporter) Step(name string) { fmt.Fprintf(&r.buf, "==> %s\n", name) }
func (r *reporter) Fail(f string, a ...any) {
	fmt.Fprintf(&r.buf, "  FAIL: %s\n", fmt.Sprintf(f, a...))
	r.fails++
}
func (r *reporter) Warn(f string, a ...any) {
	fmt.Fprintf(&r.buf, "  WARN: %s\n", fmt.Sprintf(f, a...))
	r.warns++
}

// tail is how many lines of a failing command's output are kept.
const tail = 40

func (r *reporter) done(res proc.Result, err error) bool {
	if err == nil && res.Code == 0 {
		return true
	}
	out := strings.TrimRight(res.Stdout+res.Stderr, "\n")
	if err != nil {
		out += "\n" + err.Error()
	}
	l := strings.Split(strings.TrimLeft(out, "\n"), "\n")
	if len(l) > tail {
		fmt.Fprintf(&r.buf, "  ... (%d earlier line(s) cut)\n", len(l)-tail)
		l = l[len(l)-tail:]
	}
	r.buf.WriteString(strings.Join(l, "\n") + "\n")
	return false
}

func (r *reporter) Exec(ctx context.Context, dir, name string, args ...string) bool {
	return r.done(proc.Run(ctx, dir, nil, name, args...))
}

func (r *reporter) Shell(ctx context.Context, dir, cmd string) bool {
	return r.done(proc.RunShell(ctx, dir, nil, cmd))
}

func sorted(a []string) []string {
	sort.Strings(a)
	out := a[:0]
	for i, s := range a {
		if i == 0 || s != a[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Run verifies the repository containing dir and writes the report to out. It returns the exit code.
func Run(ctx context.Context, dir string, o Options, out io.Writer) int {
	root, err := project.Root(dir)
	if err != nil {
		fmt.Fprintln(out, "verify: not in a git repository")
		return 1
	}
	s, p, _ := project.Load(root)
	st := stack.Get(s.String("STACK"))
	if s.String("STACK") == "" {
		st = stack.Get("go") // the documented default (D11); generic when the go stack is not built in
	}
	if st == nil {
		st = stack.Get("generic")
	}
	baseRef := ResolveBase(root, s)
	mb, err := gitx.MergeBase(root, baseRef, "HEAD")
	if err != nil || mb == "" || o.SinceHead {
		mb = "HEAD"
	}
	changed, _ := gitx.ChangedFiles(root, mb, true)
	own := filepath.ToSlash(p.KitDir) + "/.verify-cache/" // the cache is not part of the code state, ignored or not
	kept := changed[:0]
	for _, f := range changed {
		if !strings.HasPrefix(f, own) {
			kept = append(kept, f)
		}
	}
	changed = sorted(kept)
	if len(changed) == 0 {
		fmt.Fprintf(out, "nothing changed vs %s\n", baseRef)
		return 0
	}
	var src []string
	for _, f := range changed {
		if st.IsSource(f) {
			src = append(src, f)
		}
	}
	polDir := os.Getenv("VERIFY_POLICY_DIR")
	if polDir == "" {
		polDir = s.String("VERIFY_POLICY_DIR")
	}
	if polDir == "" {
		polDir = filepath.Join(p.Dir(), "verify.d")
	} else if !filepath.IsAbs(polDir) {
		polDir = filepath.Join(root, polDir)
	}
	policies := Policies(polDir)

	cacheDir, key := filepath.Join(p.Dir(), ".verify-cache"), ""
	if !o.NoCache {
		key = cacheKey(root, p, st.Name(), o, mb, policies)
		if b, err := os.ReadFile(filepath.Join(cacheDir, key)); err == nil {
			out.Write(b)
			fmt.Fprintln(out, "(cached: identical tree already verified green)")
			return 0
		}
	}

	r := &reporter{}
	st.Steps(ctx, stack.Env{Root: root, KitDir: p.KitDir, MergeBase: mb, Fast: o.Fast, Changed: changed, Source: src, Settings: s}, r)
	runPolicies(ctx, r, root, mb, st.Name(), changed, src, policies)
	fmt.Fprintf(&r.buf, "\nverify: %d failure(s), %d warning(s)\n", r.fails, r.warns)
	out.Write(r.buf.Bytes())
	if r.fails > 0 {
		return 1
	}
	if key != "" && os.MkdirAll(cacheDir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(cacheDir, key), r.buf.Bytes(), 0o644)
		prune(cacheDir, 20)
	}
	return 0
}

// cacheKey hashes the exact state of the tree, the flags, the settings and the policies.
func cacheKey(root string, p project.Project, stackName string, o Options, mb string, policies []Policy) string {
	h := sha256.New()
	fmt.Fprintf(h, "core2 %s fast=%t since=%t base=%s\n", stackName, o.Fast, o.SinceHead, mb)
	head, _ := gitx.Run(root, "rev-parse", "HEAD")
	diff, _ := gitx.Run(root, "diff", "HEAD")
	fmt.Fprintf(h, "%s\n%s\n", head, diff)
	un, _ := gitx.Run(root, "ls-files", "-o", "--exclude-standard")
	for _, f := range gitx.Lines(un) {
		if strings.HasPrefix(f, filepath.ToSlash(p.KitDir)+"/.verify-cache/") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(root, f))
		fmt.Fprintf(h, "%s %d %x\n", f, len(b), sha256.Sum256(b))
	}
	for _, f := range []string{p.EnvFile, "sdkit.local.env"} {
		b, _ := os.ReadFile(filepath.Join(p.Dir(), f))
		h.Write(b)
	}
	for _, pol := range policies {
		b, _ := os.ReadFile(pol.Path)
		fmt.Fprintf(h, "%s\n%s", pol.Path, b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// prune keeps the n most recent cache entries.
func prune(dir string, n int) {
	es, err := os.ReadDir(dir)
	if err != nil || len(es) <= n {
		return
	}
	sort.Slice(es, func(i, j int) bool {
		a, _ := es[i].Info()
		b, _ := es[j].Info()
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	for _, e := range es[n:] {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
