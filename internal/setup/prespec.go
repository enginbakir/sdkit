package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
)

// PrespecCheck validates the pre-spec name for the repo containing dir:
// <PRESPEC_DIR>/<name>.md (or a <name>/ directory holding markdown) exists,
// is not empty, is git-ignored and is not tracked. Validation only. It
// returns the exit code; stdout and stderr lines go to out and errw.
func PrespecCheck(dir, arg string, out, errw func(string)) int {
	name := strings.TrimSuffix(arg, ".md")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		errw(fmt.Sprintf("prespec: bad name '%s' (a plain name, no path)", arg))
		return cli.ExitUsage
	}
	top, err := project.Root(dir)
	if err != nil {
		errw("prespec: not in a git repository")
		return cli.ExitFail
	}
	s, _, _ := project.Load(top)
	pdir := strings.TrimRight(s.String("PRESPEC_DIR"), "/")
	if pdir == "" {
		pdir = "prespec"
	}
	var cands []string
	file := filepath.Join(top, filepath.FromSlash(pdir), name+".md")
	if st, e := os.Stat(file); e == nil && st.Mode().IsRegular() {
		cands = []string{pdir + "/" + name + ".md"}
	} else if root := filepath.Join(top, filepath.FromSlash(pdir), name); prespecIsDir(root) {
		// WalkDir visits in lexical order, like sort.
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e == nil && d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".md") {
				rel, _ := filepath.Rel(top, p)
				cands = append(cands, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	if len(cands) == 0 {
		errw(fmt.Sprintf("prespec: no markdown found for '%s' in %s/ (expected %s/%s.md)", name, pdir, pdir, name))
		return cli.ExitFail
	}
	found := false
	for _, f := range cands {
		b, _ := os.ReadFile(filepath.Join(top, filepath.FromSlash(f)))
		if len(strings.TrimSpace(string(b))) > 0 {
			found = true
		} else {
			errw(fmt.Sprintf("prespec: %s is empty", f))
		}
		if !gitx.IsIgnored(top, f) {
			errw(fmt.Sprintf("prespec: %s is not git-ignored (add /%s/ to .gitignore)", f, pdir))
			return cli.ExitFail
		}
		if gitx.Tracked(top, f) {
			errw(fmt.Sprintf("prespec: %s is tracked by git (git rm --cached %s); pre-specs are never committed", f, f))
			return cli.ExitFail
		}
	}
	if !found {
		errw(fmt.Sprintf("prespec: '%s' has no non-empty markdown", name))
		return cli.ExitFail
	}
	out(fmt.Sprintf("prespec: %s ok (%d file(s), git-ignored, untracked)", name, len(cands)))
	return cli.ExitOK
}

func prespecIsDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
