package adapter

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Req is what a built-in host needs to run one role.
type Req struct {
	Role, Model, Effort, Cwd, Prompt string
	LastMsg                          string // codex writes its answer to this file
	RO                               bool
}

// Cmd is a command line. Prompt is the index in Args of the prompt (shown as its length in a dry run); -1 when none.
type Cmd struct {
	CLI    string
	Args   []string
	Prompt int
}

// Build returns the headless command line of a built-in host. The flag spellings follow each tool's documented headless mode
// (core/adapters.json says which were only declared); roles never name a model, a tier or an effort does.
func Build(host string, r Req) (Cmd, bool) {
	c := Cmd{CLI: CLI(host), Prompt: -1}
	add := func(a ...string) { c.Args = append(c.Args, a...) }
	prompt := func(a ...string) {
		c.Prompt = len(c.Args) + len(a)
		add(a...)
		add(r.Prompt)
	}
	opt := func(flag, v string) {
		if v != "" {
			add(flag, v)
		}
	}
	pick := func(ro bool, a, b string) string {
		if ro {
			return a
		}
		return b
	}
	switch host {
	case "claude":
		prompt("-p")
		add("--output-format", "json")
		opt("--model", r.Model)
		if r.RO {
			add("--disallowedTools", "Edit Write NotebookEdit")
		} else {
			add("--permission-mode", "acceptEdits")
		}
	case "codex":
		add("exec", "-C", r.Cwd, "--sandbox", pick(r.RO, "read-only", "workspace-write"), "--output-last-message", r.LastMsg)
		opt("-m", r.Model)
		if r.Effort != "" {
			add("-c", "model_reasoning_effort="+r.Effort)
		}
		prompt()
	case "opencode":
		add("run", "--dir", r.Cwd, "--agent", r.Role)
		opt("-m", r.Model)
		prompt()
	case "cursor":
		prompt("-p")
		add("--output-format", "json", "--workspace", r.Cwd)
		opt("--model", r.Model)
		if r.RO {
			add("--mode", "ask")
		} else {
			add("--force")
		}
	case "gemini":
		prompt("-p")
		add("-o", "json", "--approval-mode", pick(r.RO, "plan", "auto_edit"))
		opt("-m", r.Model)
	default:
		return Cmd{}, false
	}
	return c, true
}

// Answer is a role's answer and the token usage the host printed. Tokens is false when none was read: never assume 0 is a count.
type Answer struct {
	Text                           string
	Tokens                         bool
	In, Out, CacheRead, CacheWrite int
	Cost                           string // the host's own client-side estimate, "" when absent
}

func jsonObj(raw []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func str(v any) string { s, _ := v.(string); return s }
func num(m map[string]any, k string) (int, bool) {
	f, ok := m[k].(float64)
	return int(f), ok
}

// walk sums every number found under key anywhere in v, like jq's `.. | .key?`.
func walk(v any, key string) (sum int, found bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if f, ok := e.(float64); ok && k == key {
				sum, found = sum+int(f), true
			} else if s, f2 := walk(e, key); f2 {
				sum, found = sum+s, true
			}
		}
	case []any:
		for _, e := range x {
			if s, f := walk(e, key); f {
				sum, found = sum+s, true
			}
		}
	}
	return
}

// Parse reads what a host printed: raw is its stdout, last the codex last-message file. An empty answer falls back to raw.
func Parse(host string, raw, last []byte) Answer {
	var a Answer
	m := jsonObj(raw)
	switch {
	case host == "claude" || strings.HasPrefix(host, "ext:"):
		a.Text = str(m["result"])
		u, _ := m["usage"].(map[string]any)
		in, ok1 := num(u, "input_tokens")
		out, ok2 := num(u, "output_tokens")
		a.In, a.Out, a.Tokens = in, out, ok1 && ok2
		a.CacheRead, _ = num(u, "cache_read_input_tokens")
		a.CacheWrite, _ = num(u, "cache_creation_input_tokens")
		if c, ok := m["total_cost_usd"].(float64); ok && c >= 0 && host == "claude" {
			a.Cost = strconv.FormatFloat(c, 'f', -1, 64)
		}
	case host == "cursor":
		a.Text = str(m["result"])
	case host == "gemini":
		a.Text = str(m["response"])
		var f1, f2 bool
		a.In, f1 = walk(m, "promptTokenCount")
		a.Out, _ = walk(m, "candidatesTokenCount")
		a.Tokens = f1
		_ = f2
	case host == "codex":
		a.Text = string(last)
	case host == "opencode":
		a.Text = string(raw)
	}
	a.Text = strings.TrimRight(a.Text, "\n")
	if a.Text == "" {
		a.Text = strings.TrimRight(string(raw), "\n")
	}
	return a
}
