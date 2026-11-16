package stack

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
)

// Helpers shared by the Node-based stacks (nextjs, react-native).
// Setting (sdkit.env): NODE_PM (npm|pnpm|yarn|bun; default: from the lockfile).

// pkgJSON reads <dir>/package.json; ok is false when it is missing or not JSON.
func pkgJSON(dir string) (m map[string]any, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil || json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	return m, true
}

// truthy is jq's "// empty" test: present and neither null nor false.
func truthy(v any, ok bool) bool { return ok && v != nil && v != false }

// pkgHasDep: name is listed in one of the given dependency sections of dir/package.json.
func pkgHasDep(dir, name string, sections ...string) bool {
	m, ok := pkgJSON(dir)
	if !ok {
		return false
	}
	for _, s := range sections {
		if d, _ := m[s].(map[string]any); d != nil {
			if v, ok := d[name]; truthy(v, ok) {
				return true
			}
		}
	}
	return false
}

// nodeDetect: a package.json up to three levels below root (outside node_modules) lists dep.
func nodeDetect(root, dep string, sections ...string) (found bool) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if d.IsDir() {
			if d.Name() == "node_modules" || depth > 2 {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "package.json" && pkgHasDep(filepath.Dir(p), dep, sections...) {
			found = true
		}
		return nil
	})
	return found
}

// nodePkgOf: nearest directory (slash path relative to root) holding a package.json, "" if none.
func nodePkgOf(root, file string) string {
	dir := path.Dir(filepath.ToSlash(file))
	for {
		if isFile(filepath.Join(root, filepath.FromSlash(dir), "package.json")) {
			return dir
		}
		if dir == "." || dir == "/" {
			break
		}
		dir = path.Dir(dir)
	}
	if isFile(filepath.Join(root, "package.json")) {
		return "."
	}
	return ""
}

// nodePackages: the sorted packages that own any of the (existing) files.
func nodePackages(root string, files []string) []string {
	set := map[string]bool{}
	for _, f := range files {
		if isFile(filepath.Join(root, filepath.FromSlash(f))) {
			if p := nodePkgOf(root, f); p != "" {
				set[p] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// nodePM: NODE_PM, else the nearest lockfile going up from pkg, else npm.
func nodePM(e Env, pkg string) string {
	if e.Settings != nil {
		if v := e.Settings.String("NODE_PM"); v != "" {
			return v
		}
	}
	dir := pkg
	for {
		d := filepath.Join(e.Root, filepath.FromSlash(dir))
		switch {
		case isFile(filepath.Join(d, "pnpm-lock.yaml")):
			return "pnpm"
		case isFile(filepath.Join(d, "yarn.lock")):
			return "yarn"
		case isFile(filepath.Join(d, "bun.lockb")), isFile(filepath.Join(d, "bun.lock")):
			return "bun"
		case isFile(filepath.Join(d, "package-lock.json")):
			return "npm"
		}
		if dir == "." || dir == "/" {
			return "npm"
		}
		dir = path.Dir(dir)
	}
}

func pkgDir(e Env, pkg string) string { return filepath.Join(e.Root, filepath.FromSlash(pkg)) }

func nodeHasScript(e Env, pkg, name string) bool {
	m, ok := pkgJSON(pkgDir(e, pkg))
	if !ok {
		return false
	}
	s, _ := m["scripts"].(map[string]any)
	v, ok := s[name]
	return truthy(v, ok)
}

// nodeDefaultTest: the test script is npm's "no test specified" placeholder.
func nodeDefaultTest(e Env, pkg string) bool {
	m, _ := pkgJSON(pkgDir(e, pkg))
	s, _ := m["scripts"].(map[string]any)
	t, _ := s["test"].(string)
	return strings.Contains(t, "no test specified")
}

func nodeInstalled(e Env, pkg string) bool {
	for _, d := range []string{pkgDir(e, pkg), e.Root} {
		if st, err := os.Stat(filepath.Join(d, "node_modules")); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// localBin: path of a locally installed binary (node_modules/.bin), "" if absent.
func localBin(e Env, pkg, bin string) string {
	for _, d := range []string{pkgDir(e, pkg), e.Root} {
		b := filepath.Join(d, "node_modules", ".bin", bin)
		for _, c := range []string{b, b + ".cmd"} {
			if isFile(c) {
				return c
			}
		}
	}
	return ""
}

// nodeCmd turns an executable path into a command line; on Windows .cmd/.bat shims run through cmd /C.
func nodeCmd(exe string, args []string) (string, []string) {
	if runtime.GOOS == "windows" {
		if ext := strings.ToLower(filepath.Ext(exe)); ext == ".cmd" || ext == ".bat" {
			return "cmd", append([]string{"/C", exe}, args...)
		}
	}
	return exe, args
}

// nodeExecTool runs a PATH tool (npm, npx, pnpm...), resolving shims with LookPath.
func nodeExecTool(ctx context.Context, r Reporter, dir, tool string, args ...string) bool {
	if p, err := exec.LookPath(tool); err == nil {
		tool = p
	}
	name, a := nodeCmd(tool, args)
	return r.Exec(ctx, dir, name, a...)
}

// nodeRun runs a package.json script with the package's package manager.
func nodeRun(ctx context.Context, e Env, r Reporter, pkg, script string) bool {
	return nodeExecTool(ctx, r, pkgDir(e, pkg), nodePM(e, pkg), "run", script)
}

// nodeExec runs a locally installed binary and never downloads; found is false when there is none.
func nodeExec(ctx context.Context, e Env, r Reporter, pkg, bin string, args ...string) (found, ok bool) {
	if localBin(e, pkg, bin) == "" {
		return false, false
	}
	dir := pkgDir(e, pkg)
	switch nodePM(e, pkg) {
	case "pnpm":
		return true, nodeExecTool(ctx, r, dir, "pnpm", append([]string{"exec", bin}, args...)...)
	case "yarn":
		return true, nodeExecTool(ctx, r, dir, "yarn", append([]string{bin}, args...)...)
	case "bun":
		return true, nodeExecTool(ctx, r, dir, "bunx", append([]string{"--no-install", bin}, args...)...)
	}
	return true, nodeExecTool(ctx, r, dir, "npx", append([]string{"--no-install", bin}, args...)...)
}

// nodeCheck runs script if package.json has it, else the local binary, else warns and skips.
func nodeCheck(ctx context.Context, e Env, r Reporter, pkg, label, script, bin string, args ...string) {
	if !nodeInstalled(e, pkg) {
		r.Warn("%s skipped in %s: dependencies not installed (%s install)", label, pkg, nodePM(e, pkg))
		return
	}
	if nodeHasScript(e, pkg, script) {
		if !nodeRun(ctx, e, r, pkg, script) {
			r.Fail("%s: %s", label, pkg)
		}
		return
	}
	found, ok := nodeExec(ctx, e, r, pkg, bin, args...)
	switch {
	case !found:
		r.Warn("%s skipped in %s: no '%s' script and no local %s", label, pkg, script, bin)
	case !ok:
		r.Fail("%s: %s", label, pkg)
	}
}

// nodeFmt formats one file with the nearest local prettier; it never fails.
func nodeFmt(file string, exts *regexp.Regexp, mustExist bool) error {
	if !exts.MatchString(file) || (mustExist && !isFile(file)) {
		return nil
	}
	d := filepath.Dir(file)
	for {
		for _, b := range []string{"prettier", "prettier.cmd"} {
			p := filepath.Join(d, "node_modules", ".bin", b)
			if isFile(p) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				name, a := nodeCmd(p, []string{"--write", file})
				_, _ = proc.Run(ctx, "", nil, name, a...)
				return nil
			}
		}
		if up := filepath.Dir(d); d == "." || up == d {
			return nil
		}
		d = filepath.Dir(d)
	}
}

func nodeTools(extra ...Tool) []Tool {
	return append([]Tool{{true, "node", "install Node.js (see .nvmrc or engines in package.json)"}}, extra...)
}
