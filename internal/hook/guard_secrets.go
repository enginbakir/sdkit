package hook

import (
	"runtime"

	"github.com/wecii/sdkit/internal/glob"
	"github.com/wecii/sdkit/internal/project"
)

// foldCase makes protected-path comparisons case-insensitive on Windows.
var foldCase = runtime.GOOS == "windows"

func init() {
	Register(PreToolUse, []string{"Read", "Edit", "Write"}, "guard-secrets", guardSecrets)
}

// kitEnvPath is "<project dir>/<settings file>" as the messages name it.
func kitEnvPath(in *Input) string {
	p := project.Resolve(in.Root)
	return p.KitDir + "/" + p.EnvFile
}

// guardSecrets denies access to files matching SECRET_GLOBS (basename or whole path).
func guardSecrets(in *Input) Decision {
	if in.Path == "" || !glob.MatchList(in.Path, in.Settings().String("SECRET_GLOBS"), foldCase) {
		return Decision{}
	}
	return Decision{Kind: Deny, Reason: "'" + in.Path + "' matches SECRET_GLOBS in " + kitEnvPath(in) + ". Secrets must not be read or written by the agent."}
}
