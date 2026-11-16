package stack

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
	"github.com/wecii/sdkit/internal/proc"
)

func init() { Register(goStack{}) }

type goStack struct{}

func (goStack) Name() string { return "go" }

func (goStack) Tools() []Tool {
	return []Tool{
		{true, "go", "install Go (see go.mod for the version)"},
		{false, "golangci-lint", "brew install golangci-lint (lint is skipped without it)"},
	}
}

func (goStack) IsSource(p string) bool { return strings.HasSuffix(p, ".go") }

// Detect: a go.mod within three levels of root, outside node_modules.
func (goStack) Detect(root string) bool {
	return findFile(root, 3, func(n string) bool { return n == "go.mod" }, "node_modules")
}

func (goStack) Fmt(file string) error {
	if !strings.HasSuffix(file, ".go") {
		return nil
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	if _, err := proc.LookPath("gofmt"); err != nil {
		return nil
	}
	_, _ = proc.Run(context.Background(), "", nil, "gofmt", "-w", file)
	return nil
}

// findFile walks root up to depth directory levels (find -maxdepth) skipping the named dirs.
func findFile(root string, depth int, match func(string) bool, skip ...string) bool {
	var walk func(dir string, d int) bool
	walk = func(dir string, d int) bool {
		es, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, e := range es {
			if e.IsDir() {
				skipped := false
				for _, s := range skip {
					skipped = skipped || e.Name() == s
				}
				if !skipped && d < depth && walk(filepath.Join(dir, e.Name()), d+1) {
					return true
				}
			} else if match(e.Name()) {
				return true
			}
		}
		return false
	}
	return walk(root, 1)
}

// moduleOf is the nearest directory (slash form, "." for root) holding a go.mod, or "".
func moduleOf(root, file string) string {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir), "go.mod")); err == nil {
			return dir
		}
		if dir == "." || dir == "/" {
			return ""
		}
	}
}

func (goStack) Steps(ctx context.Context, e Env, r Reporter) {
	lint := e.Settings.String("LINT_CONFIG")
	if lint == "" {
		lint = path.Join(e.KitDir, "golangci.yml")
	}
	var lintArg string
	if _, err := os.Stat(filepath.Join(e.Root, filepath.FromSlash(lint))); err == nil {
		lintArg = "--config=" + filepath.Join(e.Root, filepath.FromSlash(lint))
	}

	set := map[string]bool{}
	for _, f := range e.Source {
		if _, err := os.Stat(filepath.Join(e.Root, filepath.FromSlash(f))); err == nil {
			if m := moduleOf(e.Root, f); m != "" {
				set[m] = true
			}
		}
	}
	// a change to the shared module must rebuild everything that depends on it
	if shared := e.Settings.String("SHARED_MODULE"); shared != "" && set[shared] {
		for _, g := range strings.Fields(e.Settings.String("DEPENDENT_MODULES_GLOB")) {
			ms, _ := filepath.Glob(filepath.Join(e.Root, filepath.FromSlash(g)))
			for _, m := range ms {
				if _, err := os.Stat(filepath.Join(m, "go.mod")); err == nil {
					rel, _ := filepath.Rel(e.Root, m)
					set[filepath.ToSlash(rel)] = true
				}
			}
		}
	}
	mods := make([]string, 0, len(set))
	for m := range set {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	r.Step("gofmt")
	for _, f := range e.Source {
		abs := filepath.Join(e.Root, filepath.FromSlash(f))
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		if res, _ := proc.Run(ctx, e.Root, nil, "gofmt", "-l", abs); strings.TrimSpace(res.Stdout) != "" {
			r.Fail("gofmt: %s", f)
		}
	}

	integ := e.Settings.String("INTEGRATION_MODULE_GLOB")
	if integ == "" {
		integ = "tests/*"
	}
	for _, m := range mods {
		dir := filepath.Join(e.Root, filepath.FromSlash(m))
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			continue
		}
		r.Step("module " + m + ": vet + build")
		if !r.Exec(ctx, dir, "go", "vet", "./...") {
			r.Fail("go vet: %s", m)
		}
		if !r.Exec(ctx, dir, "go", "build", "./...") {
			r.Fail("go build: %s", m)
		}
		// integration modules hit real infrastructure: vetted and built, never run here
		if e.Fast || glob.Match(integ, m) {
			continue
		}
		r.Step("module " + m + ": test")
		if !r.Exec(ctx, dir, "go", "test", "./...") {
			r.Fail("go test: %s", m)
		}
		if _, err := proc.LookPath("golangci-lint"); err != nil {
			r.Warn("golangci-lint not installed, lint skipped (brew install golangci-lint)")
			continue
		}
		r.Step("module " + m + ": golangci-lint (new code only)")
		args := []string{"run"}
		if lintArg != "" {
			args = append(args, lintArg)
		}
		args = append(args, "--new-from-rev="+e.MergeBase, "./...")
		if !r.Exec(ctx, dir, "golangci-lint", args...) {
			r.Fail("golangci-lint: %s", m)
		}
	}
}
