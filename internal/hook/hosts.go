package hook

import (
	"fmt"
	"io"
	"os"
)

func str(m map[string]any, keys ...string) string {
	in := Input{Raw: m}
	return in.Str(keys...)
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalise turns a host's JSON into an Input in the Claude shape (as hooks/adapt.sh did).
// ok is false for a tool the kit does not guard on that host.
func normalise(host string, m map[string]any) (*Input, bool) {
	if host == "" {
		host = first(os.Getenv("SDKIT_HOST"), "claude")
	}
	in := &Input{Event: PreToolUse, Host: host, Raw: m}
	hostEvent := str(m, "hook_event_name")
	tool, path, cmd, override := str(m, "tool_name"), "", "", ""
	switch host {
	case "gemini":
		tool = map[string]string{"read_file": "Read", "read_many_files": "Read", "write_file": "Write",
			"replace": "Edit", "run_shell_command": "Bash"}[tool]
		path = first(str(m, "tool_input", "file_path"), str(m, "tool_input", "absolute_path"), str(m, "tool_input", "path"))
		cmd = str(m, "tool_input", "command")
	case "cursor":
		switch hostEvent {
		case "beforeShellExecution":
			tool, cmd = "Bash", str(m, "command")
		case "beforeReadFile":
			tool, path = "Read", str(m, "file_path")
		case "afterFileEdit":
			tool, path = "Edit", str(m, "file_path")
		default:
			tool = map[string]string{"Shell": "Bash", "Read": "Read", "Write": "Write", "Delete": "Write"}[tool]
			path = first(str(m, "tool_input", "file_path"), str(m, "tool_input", "path"))
			cmd = str(m, "tool_input", "command")
		}
	default: // claude, opencode, codex: already the Claude shape
		in.Event = first(hostEvent, PreToolUse)
		path, cmd = str(m, "tool_input", "file_path"), str(m, "tool_input", "command")
	}
	if tool == "" && host != "claude" && host != "opencode" && host != "codex" {
		return nil, false
	}
	if host == "gemini" || host == "cursor" {
		override = first(os.Getenv("CURSOR_PROJECT_DIR"), os.Getenv("GEMINI_PROJECT_DIR"), firstRoot(m), str(m, "cwd"))
	}
	in.Tool, in.Path, in.Command, in.Root = tool, slash(path), cmd, projectRoot(override)
	in.AgentType = first(str(m, "subagent_type"), str(m, "agent_type"))
	in.Raw["hook_host_event"] = hostEvent
	return in, true
}

func firstRoot(m map[string]any) string {
	if r, _ := m["workspace_roots"].([]any); len(r) > 0 {
		s, _ := r[0].(string)
		return s
	}
	return ""
}

// emitOther translates a deny or ask for hosts that have their own protocol.
func emitOther(in *Input, d Decision, stdout, stderr io.Writer) int {
	if d.Kind != Deny && d.Kind != Ask {
		return 0
	}
	reason := d.Reason
	if in.Host == "gemini" {
		if d.Kind == Ask {
			reason = "needs explicit approval: " + reason
		}
		fmt.Fprintln(stderr, reason)
		return 2
	}
	perm := "deny"
	if d.Kind == Ask && str(in.Raw, "hook_host_event") == "beforeShellExecution" {
		perm = "ask"
	} else if d.Kind == Ask {
		reason = "needs explicit approval: " + reason
	}
	write(stdout, struct {
		Permission   string `json:"permission"`
		UserMessage  string `json:"user_message"`
		AgentMessage string `json:"agent_message"`
	}{perm, reason, reason})
	return 0
}
