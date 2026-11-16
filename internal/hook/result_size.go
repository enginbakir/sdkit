package hook

import (
	"fmt"
	"strconv"
	"strings"
)

func init() { Register(SubagentStop, nil, "result-size", resultSize) }

// resultSize warns when a role's final answer is longer than SUBAGENT_RESULT_MAX_LINES (off
// unless that is a positive integer). It never blocks.
func resultSize(in *Input) Decision {
	max, err := strconv.Atoi(setting(in, "SUBAGENT_RESULT_MAX_LINES"))
	if err != nil || max <= 0 {
		return Decision{}
	}
	text := lastText(in)
	if text == "" {
		return Decision{}
	}
	n := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	if n <= max {
		return Decision{}
	}
	role := roleOf(first(in.Str("agent_type"), in.Str("subagent_type")))
	if role == "" {
		role = "subagent"
	}
	return Decision{Message: fmt.Sprintf("%s returned %d lines (limit %d). Ask it for a short summary and have it write the full text under work/<slug>/out/ (handoff field OUT).", role, n, max)}
}
