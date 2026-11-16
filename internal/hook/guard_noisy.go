package hook

import (
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
)

func init() { Register(PreToolUse, []string{"Bash"}, "guard-noisy", guardNoisy) }

var (
	noisyCut   = regexp.MustCompile(`\|[[:space:]]*(tail|head|grep|wc|tee [^|]*>)|>[[:space:]]*[^&[:space:]]`)
	noisyLead  = regexp.MustCompile(`^[[:space:]]+`)
	noisyStrip = regexp.MustCompile(`^(env |time |cd [^;&|]+(&&|;)[[:space:]]*)`)
)

// guardNoisy asks to run a command matching NOISY_CMD_PATTERNS through the capped runner.
// It is a cost nudge, never a safety gate: off with SDD_MODE=off, for subagents and for
// commands that already cut their output.
func guardNoisy(in *Input) Decision {
	s := in.Settings()
	pats := s.String("NOISY_CMD_PATTERNS")
	if pats == "" || s.String("SDD_MODE") == "off" || in.AgentType != "" || in.Command == "" {
		return Decision{}
	}
	if strings.Contains(in.Command, "capped.sh") || noisyCut.MatchString(in.Command) {
		return Decision{}
	}
	cmd := noisyStrip.ReplaceAllString(noisyLead.ReplaceAllString(in.Command, ""), "")
	if !glob.MatchList(cmd, pats, false) {
		return Decision{}
	}
	return Decision{Kind: Ask, Reason: "Long output stays in the conversation and is re-read on every later turn. Run it through the capped runner: <kit>/scripts/capped.sh -- " + in.Command}
}
