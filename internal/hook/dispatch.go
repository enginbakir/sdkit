package hook

import (
	"encoding/json"
	"fmt"
	"io"
)

// Run is the entry point of `sdkit hook`: host is the --host value (may be empty),
// target an event name or a guard name. It returns the process exit code, which is
// only ever 0 or 2 (Claude Code on Windows treats other codes as errors).
func Run(host, target string, stdin []byte, stdout, stderr io.Writer) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	raw, ok := parse(stdin)
	if !ok {
		return 0
	}
	in, ok := normalise(host, raw)
	if !ok {
		return 0
	}
	var ds []Decision
	for _, e := range selectGuards(target, in.Tool) {
		ds = append(ds, safely(e.fn, in))
	}
	d := Merge(ds...)
	if in.Event == "" || d == (Decision{}) {
		return 0
	}
	return emit(in, d, stdout, stderr)
}

func safely(fn Guard, in *Input) (d Decision) {
	defer func() {
		if recover() != nil {
			d = Decision{}
		}
	}()
	return fn(in)
}

type hso struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

func write(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintln(w, string(b))
}

// emit writes the decision in the protocol of the host and returns the exit code.
func emit(in *Input, d Decision, stdout, stderr io.Writer) int {
	switch in.Host {
	case "gemini", "cursor":
		return emitOther(in, d, stdout, stderr)
	}
	switch {
	case d.Kind == Block:
		write(stdout, map[string]string{"decision": "block", "reason": d.Reason})
	case in.Event == PreToolUse && (d.Kind == Deny || d.Kind == Ask):
		decision := map[Kind]string{Deny: "deny", Ask: "ask"}[d.Kind]
		write(stdout, map[string]any{"hookSpecificOutput": hso{PreToolUse, decision, d.Reason, ""}})
	case in.Event == SessionStart && d.Context != "":
		write(stdout, map[string]any{"hookSpecificOutput": hso{HookEventName: SessionStart, AdditionalContext: d.Context}})
	}
	if d.Message != "" {
		write(stdout, map[string]string{"systemMessage": d.Message})
	}
	return 0
}
