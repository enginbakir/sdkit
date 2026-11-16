package verify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/proc"
)

// Policy is one file of <project-dir>/verify.d.
type Policy struct{ Name, Path string }

// Policies lists the policies of dir in name order. Names starting with "_" (helpers), "*.example" and
// extensions the platform cannot run are skipped (03-cross-platform.md item 11).
func Policies(dir string) []Policy {
	es, _ := os.ReadDir(dir)
	var out []Policy
	for _, e := range es {
		n := e.Name()
		ext := strings.ToLower(filepath.Ext(n))
		ok := ext == ".sh" || ext == ".ps1" || (runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".exe"))
		if e.IsDir() || strings.HasPrefix(n, "_") || !ok {
			continue
		}
		out = append(out, Policy{strings.TrimSuffix(n, filepath.Ext(n)), filepath.Join(dir, n)})
	}
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i].Path) < filepath.Base(out[j].Path) })
	return out
}

func command(ctx context.Context, path string) (*exec.Cmd, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ps1":
		for _, ps := range []string{"pwsh", "powershell"} {
			if p, err := proc.LookPath(ps); err == nil {
				return exec.CommandContext(ctx, p, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path), nil
			}
		}
		return nil, fmt.Errorf("no pwsh or powershell on PATH")
	case ".cmd":
		return exec.CommandContext(ctx, "cmd", "/C", path), nil
	case ".exe":
		return exec.CommandContext(ctx, path), nil
	}
	sh, err := proc.LookPath("sh")
	if err != nil {
		return nil, fmt.Errorf("no sh on PATH")
	}
	return exec.CommandContext(ctx, sh, path), nil
}

// lib is VERIFY_LIB: helpers for .sh policies, written to a temp file because the kit no longer ships verify-lib.sh.
const lib = `added_lines() {
  git diff -U0 "$VERIFY_MERGE_BASE" -- "$@" | grep '^+[^+]' || true
  for f in $(git ls-files -o --exclude-standard -- "$@"); do
    [ -f "$f" ] && sed 's/^/+/' "$f"
  done
}
`

func runPolicies(ctx context.Context, r *reporter, root, mb, stackName string, changed, src []string, pols []Policy) {
	if len(pols) == 0 {
		return
	}
	env := []string{"VERIFY_ROOT=" + root, "VERIFY_MERGE_BASE=" + mb,
		"VERIFY_CHANGED=" + strings.Join(changed, "\n"), "VERIFY_CHANGED_SRC=" + strings.Join(src, "\n")}
	if stackName == "go" {
		env = append(env, "VERIFY_CHANGED_GO="+strings.Join(src, "\n"))
	}
	if d, err := os.MkdirTemp("", "sdkit-verify-"); err == nil {
		defer os.RemoveAll(d)
		l := filepath.Join(d, "verify-lib.sh")
		if os.WriteFile(l, []byte(lib), 0o644) == nil {
			env = append(env, "VERIFY_LIB="+l)
		}
	}
	for _, p := range pols {
		r.Step("policy " + p.Name)
		cmd, err := command(ctx, p.Path)
		if err != nil {
			r.Fail("policy %s: %v", filepath.Base(p.Path), err)
			continue
		}
		res, err := proc.Run(ctx, root, env, cmd.Path, cmd.Args[1:]...)
		out := strings.TrimRight(res.Stdout+res.Stderr, "\n")
		if out != "" {
			r.buf.WriteString(out + "\n")
		}
		hasFail := false
		for _, l := range strings.Split(out, "\n") {
			switch {
			case strings.HasPrefix(l, "FAIL: "):
				r.fails++
				hasFail = true
			case strings.HasPrefix(l, "WARN: "):
				r.warns++
			}
		}
		if (err != nil || res.Code != 0) && !hasFail {
			r.Fail("policy %s exited %d", filepath.Base(p.Path), res.Code)
		}
	}
}
