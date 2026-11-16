package hook

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/project"
)

func init() {
	Register(PreToolUse, []string{"Edit", "Write"}, "frozen-tests", frozenTests)
}

// frozenTests denies edits to the files listed in <project-dir>/.frozen (one
// repo-relative path per line), which spec-driven-dev appends the regression test to.
func frozenTests(in *Input) Decision {
	b, err := os.ReadFile(filepath.Join(project.Resolve(in.Root).Dir(), ".frozen"))
	if err != nil || in.Path == "" {
		return Decision{}
	}
	rel := relPath(in.Root, in.Path)
	for _, l := range strings.Split(string(b), "\n") {
		if foldEq(strings.TrimSuffix(l, "\r"), rel) {
			return Decision{Kind: Deny, Reason: "'" + rel + "' is frozen by spec-driven-dev. Fix the implementation, not the test."}
		}
	}
	return Decision{}
}
