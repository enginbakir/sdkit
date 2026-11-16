// Package base resolves the ref a branch is diffed against and branched from.
package base

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
)

// Resolve returns the diff base of the repository at root. get reads a setting
// ("" when unset). Order: SDKIT_CI_BASE (the base a CI job gave), DIFF_BASE_CMD
// (a command string, run in root), then the branch flow: VERIFY_BASE, the newest
// release branch HEAD descends from (BASE_BRANCH_MODE=release), origin/$BASE_BRANCH.
func Resolve(root string, get func(string) string) string {
	if b := get("SDKIT_CI_BASE"); b != "" {
		return b
	}
	if c := get("DIFF_BASE_CMD"); c != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if r, err := proc.RunShell(ctx, root, nil, c); err == nil && r.Code == 0 {
			if b := strings.TrimSpace(r.Stdout); b != "" {
				return b
			}
		}
	}
	return Flow(root, get)
}

// Flow is the branch flow alone (no CI base, no DIFF_BASE_CMD).
func Flow(root string, get func(string) string) string {
	if b := get("VERIFY_BASE"); b != "" {
		return b
	}
	branch := get("BASE_BRANCH")
	if branch == "" {
		branch = "develop"
	}
	if get("BASE_BRANCH_MODE") == "release" {
		if r := pickRelease(root, prefix(get)); r != "" {
			return r
		}
	}
	if gitx.RevParse(root, "origin/"+branch) != "" {
		return "origin/" + branch
	}
	return branch
}

func prefix(get func(string) string) string {
	if p := get("RELEASE_BRANCH_PREFIX"); p != "" {
		return p
	}
	return "release/"
}

// Frozen reports "frozen" when the release base has a freeze tag, else "open".
func Frozen(root string, get func(string) string, base string) string {
	fz := get("FREEZE_TAG_PREFIX")
	if fz == "" {
		fz = "freeze/"
	}
	name := strings.TrimPrefix(base, "origin/"+prefix(get))
	if _, err := gitx.Run(root, "rev-parse", "--verify", "-q", "refs/tags/"+fz+name); err == nil {
		return "frozen"
	}
	return "open"
}

// pickRelease: of the origin release branches sharing history with HEAD, the one
// with the fewest commits beyond the fork point; newer names win ties.
func pickRelease(root, prefix string) string {
	out, err := gitx.Run(root, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/"+prefix+"*")
	if err != nil {
		return ""
	}
	refs := gitx.Lines(out)
	sortV(refs)
	best, bestDist := "", -1
	for _, ref := range refs {
		fork, err := gitx.MergeBase(root, ref, "HEAD")
		if err != nil {
			continue
		}
		c, err := gitx.Run(root, "rev-list", "--count", fork+"..HEAD")
		d, e2 := strconv.Atoi(c)
		if err != nil || e2 != nil {
			continue
		}
		if bestDist < 0 || d <= bestDist {
			best, bestDist = ref, d
		}
	}
	return best
}

// sortV orders strings like `sort -V`: digit runs compare as numbers.
func sortV(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && lessV(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func lessV(a, b string) bool {
	for a != "" && b != "" {
		da, db := digits(a), digits(b)
		if da > 0 && db > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[da:], b[db:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// Getter reads a setting from the file settings first, then the environment.
func Getter(raw func(string) (string, bool)) func(string) string {
	return func(k string) string {
		if v, ok := raw(k); ok && v != "" {
			return v
		}
		return os.Getenv(k)
	}
}
