// Package hook is the hook framework: it reads a host's tool-call JSON, runs the
// registered guards and answers in the host's protocol (docs/go-port/02-contracts.md,
// "Hook protocol"). Guards live in guard_<name>.go files and register from init().
// A hook never blocks because of its own error: any failure ends in exit 0, no output.
package hook

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
)

// Events a guard can register for.
const (
	PreToolUse   = "PreToolUse"
	PostToolUse  = "PostToolUse"
	SessionStart = "SessionStart"
	Stop         = "Stop"
	SubagentStop = "SubagentStop"
)

// Kind is what a guard decides. The zero value is Allow.
type Kind int

// Kinds, weakest first; the dispatcher keeps the strongest (deny > ask > allow).
const (
	Allow Kind = iota
	Ask
	Deny
	Block // Stop and SubagentStop only: {"decision":"block"}; ranks with Deny
)

// Decision is one guard's answer. Allow with no Context or Message is silent.
type Decision struct {
	Kind    Kind
	Reason  string
	Context string // SessionStart additionalContext
	Message string // {"systemMessage": ...}
}

// Input is the tool call, already normalised to the Claude shape.
type Input struct {
	Event     string
	Host      string // claude, gemini, cursor, opencode ...; --host, else SDKIT_HOST, else claude
	Tool      string // Read, Write, Edit, Bash, Agent ...
	Path      string // tool_input.file_path, with / separators
	Command   string // tool_input.command
	AgentType string
	Root      string // project dir: CLAUDE_PROJECT_DIR, else the git top level, else the cwd
	Raw       map[string]any

	settings *envfile.Settings
}

// Str returns the string at a path of keys in the raw JSON ("" when absent).
func (in *Input) Str(keys ...string) string {
	var v any = in.Raw
	for _, k := range keys {
		m, _ := v.(map[string]any)
		v = m[k]
	}
	s, _ := v.(string)
	return s
}

// Settings loads sdkit.env and sdkit.local.env of the project once.
func (in *Input) Settings() *envfile.Settings {
	if in.settings == nil {
		in.settings, _, _ = project.Load(in.Root)
	}
	return in.settings
}

// slash turns a Windows path into a / path, on every OS (hosts may send C:\...).
func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

func projectRoot(override string) string {
	if override != "" {
		if st, err := os.Stat(override); err == nil && st.IsDir() {
			return slash(override)
		}
	}
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" {
		return slash(d)
	}
	cwd, _ := os.Getwd()
	if top, err := gitx.Top(cwd); err == nil && top != "" {
		return slash(top)
	}
	return slash(cwd)
}

func parse(stdin []byte) (map[string]any, bool) {
	m := map[string]any{}
	return m, json.Unmarshal(stdin, &m) == nil
}

// Merge keeps the strongest decision (deny > ask > allow) and joins the context and
// message texts. Ties keep the first reason.
func Merge(ds ...Decision) Decision {
	var out Decision
	for i, d := range ds {
		if i == 0 || rank(d.Kind) > rank(out.Kind) {
			out.Kind, out.Reason = d.Kind, d.Reason
		}
		out.Context = join(out.Context, d.Context)
		out.Message = join(out.Message, d.Message)
	}
	return out
}

func rank(k Kind) int {
	if k == Block {
		return int(Deny)
	}
	return int(k)
}

func join(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + "\n" + b
}
