package hook

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
)

func init() {
	Register(PreToolUse, []string{"Edit", "Write"}, "guard-protected", guardProtected)
}

// relPath is p relative to root with //, . and .. resolved and symlinks followed where
// they exist (hooks/lib.sh sdkit_relpath). A path outside root comes back absolute.
func relPath(root, p string) string {
	if !filepath.IsAbs(filepath.FromSlash(p)) {
		p = root + "/" + p
	}
	q, rest := filepath.FromSlash(path.Clean(p)), ""
	for { // follow symlinks of the deepest part that exists
		if r, err := filepath.EvalSymlinks(q); err == nil {
			q = filepath.Join(r, rest)
			break
		}
		d := filepath.Dir(q)
		if d == q {
			q = filepath.Join(q, rest)
			break
		}
		rest, q = filepath.Join(filepath.Base(q), rest), d
	}
	if r, err := filepath.EvalSymlinks(filepath.FromSlash(root)); err == nil {
		root = r
	}
	q, root = filepath.ToSlash(q), strings.TrimRight(filepath.ToSlash(root), "/")
	switch {
	case foldEq(q, root):
		return ""
	case len(q) > len(root) && foldEq(q[:len(root)+1], root+"/"):
		return q[len(root)+1:]
	}
	return q
}

func foldEq(a, b string) bool {
	if foldCase {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// guardProtected asks before an edit under a PROTECTED_PATHS prefix.
func guardProtected(in *Input) Decision {
	list := in.Settings().String("PROTECTED_PATHS")
	if in.Path == "" || list == "" {
		return Decision{}
	}
	rel := relPath(in.Root, in.Path)
	for _, prefix := range strings.Split(list, ",") {
		if prefix != "" && glob.HasDirPrefix(rel, prefix, foldCase) {
			return Decision{Kind: Ask, Reason: "'" + prefix + "' is listed in PROTECTED_PATHS (" + kitEnvPath(in) + "). Check project.md for when it may be changed."}
		}
	}
	return Decision{}
}
