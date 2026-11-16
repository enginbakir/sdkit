package hook

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/contract"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/workflow"
)

func init() {
	Register(PreToolUse, []string{"Agent", "Task"}, "check-contract", checkHandoff)
	Register(SubagentStop, nil, "check-contract", checkAnswer)
}

var contractLine = regexp.MustCompile(`(?m)^[ \t]*CONTRACT:`)

// contractsOn: off unless CONTRACTS=1, or the open task runs a profile that requires contracts.
func contractsOn(in *Input) (on bool, profile string) {
	e := sdd.New(in.Root)
	profile = state.ReadOpen(e.P).Profile
	return setting(in, "CONTRACTS") == "1" || profile == "high-assurance", profile
}

// advise is advisory: it never denies. An invalid handoff asks first in strict mode, only warns in guided mode.
func advise(in *Input, msg string) Decision {
	switch sdd.New(in.Root).Mode() {
	case "off":
		return Decision{}
	case "strict":
		return Decision{Kind: Ask, Reason: msg}
	}
	return Decision{Message: msg}
}

// checkHandoff validates and registers a contracted handoff before the agent starts (PreToolUse on the Agent tool).
func checkHandoff(in *Input) Decision {
	on, profile := contractsOn(in)
	if !on || in.Event != PreToolUse {
		return Decision{}
	}
	prompt, role := in.Str("tool_input", "prompt"), first(in.Str("tool_input", "subagent_type"), in.AgentType)
	if !contractLine.MatchString(prompt) {
		if workflow.Contracts(profile, setting(in, "CONTRACTS") == "1", role) != "required" {
			return Decision{}
		}
		return advise(in, fmt.Sprintf("profile %s requires a contract for role %s: add CONTRACT: 1, ID: and REPLY-END: to the handoff (references/contract.md).", profile, role[strings.LastIndex(role, ":")+1:]))
	}
	r := contract.New(in.Root).Issue(prompt, contract.Options{Role: role})
	if r.Exit() != 2 {
		return Decision{}
	}
	return advise(in, "Handoff contract rejected: "+r.Line()+". Fix the handoff (references/contract.md) before spawning.")
}

// checkAnswer reports a missing or malformed HANDOFF-RESULT line to the orchestrator; it never blocks and never touches the verdict.
func checkAnswer(in *Input) Decision {
	if on, _ := contractsOn(in); !on || in.Event != SubagentStop {
		return Decision{}
	}
	role := first(in.Str("agent_type"), in.Str("subagent_type"))
	role = role[strings.LastIndex(role, ":")+1:]
	r := contract.New(in.Root).CheckResult(lastText(in), contract.Options{Role: role, Cwd: in.Root, Record: true, LegacyOK: true})
	if r.Exit() != 2 {
		return Decision{}
	}
	return Decision{Message: "Agent contract NOT accepted: " + r.Line() + ". Ask the agent once for the HANDOFF-RESULT line (sdkit state <slug> retry <id> first); a contract never replaces the verifier."}
}
