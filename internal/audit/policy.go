package audit

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

var secretShape = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{24,}`)

// Policy is the static check of the enforcement the kit ships, over the kit files (embedded in the binary): a read-only role lists no
// Edit/Write/NotebookEdit tool in its generated agent, and no secret-shaped string sits in core/ or adapters/. It returns one line per violation.
// (Hook wiring and adapter validation of 1.x are gone: the guards are compiled in behind the single `sdkit hook` entry.)
func Policy(kit fs.FS) []string {
	var bad []string
	agents, _ := fs.Glob(kit, "agents/*.md")
	for _, f := range agents {
		role := strings.TrimSuffix(strings.TrimSuffix(path.Base(f), ".md"), "-deep")
		src, err := fs.ReadFile(kit, "core/roles/"+role+".md")
		if err != nil || !hasLine(string(src), "access:", "read-only") {
			continue
		}
		b, _ := fs.ReadFile(kit, f)
		for _, l := range strings.Split(string(b), "\n") {
			if t, ok := strings.CutPrefix(l, "tools:"); ok {
				if tl := strings.TrimSpace(t); strings.Contains(tl, "Edit") || strings.Contains(tl, "Write") || strings.Contains(tl, "NotebookEdit") {
					bad = append(bad, fmt.Sprintf("read-only role %s lists a write tool in %s: %s", role, f, tl))
				}
				break
			}
		}
	}
	var hits []string
	for _, root := range []string{"core", "adapters"} {
		_ = fs.WalkDir(kit, root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && len(hits) < 3 {
				if b, _ := fs.ReadFile(kit, p); secretShape.Match(b) {
					hits = append(hits, p)
				}
			}
			return nil
		})
	}
	if len(hits) > 0 {
		bad = append(bad, "secret-shaped string in: "+strings.Join(hits, " "))
	}
	return bad
}

func hasLine(s, key, val string) bool {
	for _, l := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(l, key); ok {
			return strings.TrimSpace(v) == val
		}
	}
	return false
}
