package hook

import (
	"os"
	"regexp"

	"github.com/wecii/sdkit/internal/sdd"
)

func init() {
	Register(PreToolUse, []string{"Bash"}, "guard-pr", guardPR)
}

// A regex over a command string is a speed bump, not a boundary: docs/guide.md "What the guards cannot stop".
var (
	prState = regexp.MustCompile(`\.sdd/|hooks/(record-verdict|guard-budget)\.sh`)
	// An attestation is written only by sdd, but the author commits it, so git add/commit/status/diff and a mention in a commit or PR
	// text must pass. Deny only a command that writes the file: a redirection, or a write verb, aimed at attestations/.
	prAttest = regexp.MustCompile(`(^|[^-=>])>>?[[:space:]]*[^;&|]*attestations/|(^|[^A-Za-z0-9_.-])(tee|cp|mv|rm|touch|truncate|dd|install|ln|chmod|sed[[:space:]]+-[A-Za-z]*i|perl[[:space:]]+-[A-Za-z]*i)([[:space:]]+[^;&|]*)?[[:space:]][^;&|]*attestations/`)
	// the hook-only subcommands anywhere in a command that runs the kit (1.x scripts or `sdkit ...`), not only right after it
	prKit     = regexp.MustCompile(`sdd\.sh|parse-verdict\.sh|run-role\.sh|(^|[^A-Za-z0-9_.-])sdkit(\.exe)?[[:space:]]+([^;&|]*[[:space:]])?(sdd|verdict|run-role)([[:space:]]|$)`)
	prHookCmd = regexp.MustCompile(`(^|[^A-Za-z0-9])_(verdict|evidence|spawn)([^A-Za-z0-9]|$)`)
	prVerdict = regexp.MustCompile(`parse-verdict\.sh|(^|[^A-Za-z0-9_.-])sdkit(\.exe)?[[:space:]]+verdict`)
	prRecord  = regexp.MustCompile(`--record`)
	prSkip    = regexp.MustCompile(`--no-verify|core\.hooksPath|(^|[^A-Za-z0-9_])(SDD_SKIP|GUARD_SKIP_VERIFY)=`)
	// git with options before push (git -C . push, git -c k=v push), through command/env/a path/a subshell; PR creation through the API too
	prPush = regexp.MustCompile("(^|[;&|(`'\"]|\\$\\()[[:space:]]*((command|env|exec|nohup)[[:space:]]+|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+)*([^[:space:];&|]*/)?" +
		`(git([[:space:]]+-[^[:space:]]+([[:space:]]+[^-[:space:]][^[:space:]]*)?)*[[:space:]]+push|gh[[:space:]]+pr[[:space:]]+create|glab[[:space:]]+mr[[:space:]]+create|gh[[:space:]]+api[[:space:]].*pulls|[^[:space:]]*open-pr\.sh)`)
)

const prState1 = "The workflow state and verdicts are written only by sdkit sdd and the verifier hook, never by hand."

// guardPR gates push and PR creation on a verified task and keeps the model away from the workflow state and the hook-only commands.
func guardPR(in *Input) Decision {
	c := in.Command
	if c == "" {
		return Decision{}
	}
	e := sdd.New(in.Root)
	mode := e.Mode()
	if mode == "off" || !e.P.Found { // not a kit project: nothing to enforce (the bash denied here too)
		return Decision{}
	}
	deny := func(r string) Decision { return Decision{Kind: Deny, Reason: r} }
	switch {
	case prState.MatchString(c):
		return deny(prState1)
	case prAttest.MatchString(c):
		return deny("Attestations are written only by sdkit sdd. You may git add and commit one, but not edit, copy, move or delete it.")
	case prKit.MatchString(c) && prHookCmd.MatchString(c):
		return deny(prState1)
	// Claude Code records the verdict in the SubagentStop hook, so recording by hand there would be forging one. Hosts without that
	// hook (SDKIT_HOST set by the adapters) record with `verdict parse --record`, which validates the block first.
	case os.Getenv("SDKIT_HOST") == "" && (in.Host == "" || in.Host == "claude") && prVerdict.MatchString(c) && prRecord.MatchString(c):
		return deny("On Claude Code the verifier hook records the verdict. Run the verdict parse without --record to check the block.")
	case prSkip.MatchString(c):
		// skipping the git hooks is not a way around the phase gate
		if mode == "strict" {
			return deny("Skipping the git hooks (--no-verify, core.hooksPath, SDD_SKIP, GUARD_SKIP_VERIFY) is the user's call, not the agent's.")
		}
		return Decision{Kind: Ask, Reason: "This command skips the git hooks."}
	case prPush.MatchString(c):
		ok, why := e.Gate("push", "")
		return refusal(ok, why, mode)
	}
	return Decision{}
}
