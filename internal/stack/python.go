package stack

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/proc"
)

func init() { Register(pyStack{}) }

type pyStack struct{}

func (pyStack) Name() string { return "python" }

func (pyStack) Tools() []Tool {
	return []Tool{
		{true, pyName(), "install Python 3 (see pyproject.toml for the version)"},
		{false, "ruff", "pip install ruff (lint and post-edit formatting are skipped without it)"},
		{false, "pytest", "pip install pytest (falls back to unittest discover)"},
		{false, "mypy", "pip install mypy (only used when the project configures it)"},
		{false, "uv", "brew install uv (only for projects with uv.lock)"},
	}
}

// pyName is python3 where it exists, else python (the Windows name).
func pyName() string {
	if _, err := proc.LookPath("python3"); err == nil {
		return "python3"
	}
	return "python"
}

func (pyStack) IsSource(p string) bool { return strings.HasSuffix(p, ".py") }

func (pyStack) Detect(root string) bool {
	return findFile(root, 3, func(n string) bool {
		return n == "pyproject.toml" || n == "setup.py" || n == "setup.cfg" || n == "requirements.txt"
	}, "node_modules", ".venv", "venv", ".git")
}

func (pyStack) Fmt(file string) error {
	if !strings.HasSuffix(file, ".py") {
		return nil
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	if _, err := proc.LookPath("ruff"); err == nil {
		_, _ = proc.Run(context.Background(), "", nil, "ruff", "format", "-q", file)
	} else if _, err := proc.LookPath("black"); err == nil {
		_, _ = proc.Run(context.Background(), "", nil, "black", "-q", file)
	}
	return nil
}

var pyProjectFile = regexp.MustCompile(`(^|/)(pyproject\.toml|setup\.py|setup\.cfg|requirements[^/]*\.txt|pytest\.ini|tox\.ini|mypy\.ini)$`)

func exists(root, rel string, names ...string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel), n)); err == nil {
			return true
		}
	}
	return false
}

// pyPkgOf is the nearest directory (slash form) with a Python project file, "." when none.
func pyPkgOf(root, file string) string {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if exists(root, dir, "pyproject.toml", "setup.py", "setup.cfg") {
			return dir
		}
		if dir == "." || dir == "/" {
			return "."
		}
	}
}

// pyRunner is the command prefix that runs tools in the project's environment.
func pyRunner(e Env, pkg string) []string {
	if r := e.Settings.String("PY_RUNNER"); r != "" {
		return strings.Fields(r)
	}
	if exists(e.Root, pkg, "uv.lock") || exists(e.Root, ".", "uv.lock") {
		return []string{"uv", "run"}
	}
	if exists(e.Root, pkg, "poetry.lock") || exists(e.Root, ".", "poetry.lock") {
		return []string{"poetry", "run"}
	}
	return nil
}

// pyConfigured: the project configures a tool in its pyproject or its own config file.
func pyConfigured(e Env, pkg, re string) bool {
	rx := regexp.MustCompile(re)
	for _, d := range []string{pkg, "."} {
		for _, n := range []string{"pyproject.toml", "setup.cfg", "tox.ini"} {
			if b, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(d), n)); err == nil && rx.Match(b) {
				return true
			}
		}
	}
	return false
}

func (pyStack) Steps(ctx context.Context, e Env, r Reporter) {
	pkgFiles := map[string][]string{}
	add := func(f string, src bool) {
		if _, err := os.Stat(filepath.Join(e.Root, filepath.FromSlash(f))); err != nil {
			return
		}
		pkg := pyPkgOf(e.Root, f)
		if src {
			rel := f
			if pkg != "." {
				rel = strings.TrimPrefix(f, pkg+"/")
			}
			pkgFiles[pkg] = append(pkgFiles[pkg], rel)
		} else if _, ok := pkgFiles[pkg]; !ok {
			pkgFiles[pkg] = nil
		}
	}
	for _, f := range e.Source {
		add(f, true)
	}
	for _, f := range e.Changed {
		if pyProjectFile.MatchString(f) {
			add(f, false)
		}
	}
	pkgs := make([]string, 0, len(pkgFiles))
	for p := range pkgFiles {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, pkg := range pkgs {
		files := pkgFiles[pkg]
		dir := filepath.Join(e.Root, filepath.FromSlash(pkg))
		run := pyRunner(e, pkg)
		// has: the tool runs in the project's environment
		has := func(tool string) bool {
			for _, a := range [][]string{{pyName(), "-m", tool, "--version"}, {tool, "--version"}} {
				c := append(append([]string{}, run...), a...)
				if res, err := proc.Run(ctx, dir, nil, c[0], c[1:]...); err == nil && res.Code == 0 {
					return true
				}
			}
			return false
		}
		exec := func(args ...string) bool {
			c := append(append([]string{}, run...), args...)
			return r.Exec(ctx, dir, c[0], c[1:]...)
		}

		r.Step("package " + pkg + ": syntax")
		if len(files) > 0 && !exec(append([]string{pyName(), "-m", "py_compile"}, files...)...) {
			r.Fail("syntax: %s", pkg)
		}
		if e.Fast {
			continue
		}

		r.Step("package " + pkg + ": lint")
		switch {
		case has("ruff"):
			if len(files) > 0 && !exec(append([]string{"ruff", "check"}, files...)...) {
				r.Fail("ruff: %s", pkg)
			}
		case pyConfigured(e, pkg, `tool\.(ruff|flake8)|\[flake8\]`):
			r.Warn("lint configured in %s but ruff is not installed", pkg)
		default:
			r.Warn("no linter configured in %s", pkg)
		}

		if pyConfigured(e, pkg, `tool\.mypy|\[mypy|mypy\.ini`) {
			r.Step("package " + pkg + ": mypy")
			if !has("mypy") {
				r.Warn("mypy configured in %s but not installed", pkg)
			} else if !exec(append([]string{"mypy"}, files...)...) {
				r.Fail("mypy: %s", pkg)
			}
		}

		r.Step("package " + pkg + ": test")
		switch {
		case has("pytest"):
			c := append(append([]string{}, run...), "pytest", "-q")
			res, err := proc.Run(ctx, dir, nil, c[0], c[1:]...)
			switch {
			case err == nil && res.Code == 5: // nothing collected: a warning, not a failure
				r.Warn("pytest collected no tests in %s", pkg)
			case err != nil || res.Code != 0:
				exec("pytest", "-q") // rerun so the engine prints the failure tail
				r.Fail("pytest: %s", pkg)
			}
		case exists(e.Root, pkg, "tests", "test"):
			if !exec(pyName(), "-m", "unittest", "discover", "-q") {
				r.Fail("unittest: %s", pkg)
			}
		default:
			r.Warn("no tests found in %s", pkg)
		}
	}
}
