package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
)

const excludeHeader = "# sdkit: generated files"

// ExcludeUntracked hides generated files from git for this clone only (.git/info/exclude), never the
// project's .gitignore. A path git already tracks is left alone (an ignore rule would not apply to it).
func ExcludeUntracked(target string, paths ...string) error {
	ex, err := gitx.Run(target, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return nil // not a repository: nothing to hide from
	}
	if !filepath.IsAbs(ex) {
		ex = filepath.Join(target, ex)
	}
	if err := os.MkdirAll(filepath.Dir(ex), 0o755); err != nil {
		return err
	}
	b, _ := os.ReadFile(ex)
	text := string(b)
	lines := map[string]bool{}
	for _, l := range splitLines(text) {
		lines[l] = true
	}
	add := ""
	for _, p := range paths {
		p = filepath.ToSlash(p)
		if gitx.Tracked(target, p) || lines["/"+p] {
			continue
		}
		if !strings.Contains("\n"+text+add, "\n"+excludeHeader) {
			add += "\n" + excludeHeader + " (rebuilt by sdkit init)\n"
		}
		add += "/" + p + "\n"
		lines["/"+p] = true
	}
	if text != "" && !strings.HasSuffix(text, "\n") && add != "" {
		text += "\n"
	}
	return os.WriteFile(ex, []byte(text+add), 0o644)
}
