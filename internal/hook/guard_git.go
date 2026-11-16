package hook

import (
	kbase "github.com/wecii/sdkit/internal/base"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
)

func init() { Register(PreToolUse, []string{"Bash"}, "guard-git", guardGit) }

var gitStart = regexp.MustCompile(`git +(?:checkout +-b|switch +-c|branch) +[^ ;&|]+ +([^ ;&|-][^ ;&|]*)`)
var gitCreate = regexp.MustCompile(`git +(checkout +-b|switch +-c) +[^ ;&|]+`)

// guardGit enforces the release-branch flow (BASE_BRANCH_MODE=release; off otherwise):
// deny bringing the base branch into a feature branch, deny cutting a branch from the
// base, ask before pushing straight to a PROTECTED_BRANCHES env branch.
func guardGit(in *Input) Decision {
	s := in.Settings()
	if s.String("BASE_BRANCH_MODE") != "release" || !strings.Contains(in.Command, "git") {
		return Decision{}
	}
	base := s.String("BASE_BRANCH")
	if base == "" {
		base = "develop"
	}
	prefix := s.String("RELEASE_BRANCH_PREFIX")
	if prefix == "" {
		prefix = "release/"
	}
	current, _ := gitx.Run(in.Root, "rev-parse", "--abbrev-ref", "HEAD")
	release := strings.TrimSpace(kbase.Resolve(in.Root, kbase.Getter(s.Raw))) // as 1.x: resolve-base.sh
	if release == "" {
		release = "the release branch"
	}
	envs := strings.Split(s.String("PROTECTED_BRANCHES"), ",")
	baseRef := `(?:origin/)?` + regexp.QuoteMeta(base)

	feature := current != base && !strings.HasPrefix(current, prefix) && current != "HEAD" && current != ""
	for _, e := range envs {
		if e == current {
			feature = false
		}
	}
	if feature && regexp.MustCompile(`git +(merge|pull|rebase)( +-[-a-zA-Z]+)* +(origin +)?`+baseRef+`( |$|;|&)`).MatchString(in.Command) {
		return Decision{Kind: Deny, Reason: "Do not bring '" + base + "' into feature branch '" + current + "': it carries work that has not passed test into the release. Rebase/merge onto " + release + " only (see the release flow in " + project.Resolve(in.Root).KitDir + "/project.md)."}
	}
	for _, line := range strings.Split(in.Command, "\n") {
		if !gitCreate.MatchString(line) {
			continue
		}
		fromBase := current == base
		if m := gitStart.FindStringSubmatch(line); m != nil {
			fromBase = regexp.MustCompile(`^` + baseRef + `$`).MatchString(m[1])
		}
		if fromBase {
			return Decision{Kind: Deny, Reason: "Cut feature branches from the active release branch, not '" + base + "': git checkout -b <name> " + release + " (see the release flow in " + project.Resolve(in.Root).KitDir + "/project.md)."}
		}
		break
	}
	for _, e := range envs {
		if e != "" && regexp.MustCompile(`git +push\b[^;&|]*[ :]`+regexp.QuoteMeta(e)+`( |$|;|&)`).MatchString(in.Command) {
			return Decision{Kind: Ask, Reason: "Pushing straight to env branch '" + e + "'. These branches move only through the project's release tooling."}
		}
	}
	return Decision{}
}
