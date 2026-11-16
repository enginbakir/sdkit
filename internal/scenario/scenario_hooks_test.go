package scenario

import (
	"strings"
	"testing"
)

// C: hook JSON in and out for the four hosts; malformed input and unknown tools are silent exit 0.
func TestScenarioC(t *testing.T) {
	t.Parallel()
	repo := kit(t, "")
	guided := kit(t, "SDD_MODE=guided\n")
	for _, c := range []struct {
		name, repo, host, in string
		code                 int
		out                  string
	}{
		{"claude deny", repo, "claude", pre("Write", "file_path", "src/a.go"), 0, `"permissionDecision":"deny"`},
		{"claude allow", repo, "claude", pre("Write", "file_path", "docs/a.md"), 0, ""},
		{"claude ask", guided, "claude", pre("Edit", "file_path", "src/a.go"), 0, `"permissionDecision":"ask"`},
		{"claude secret", repo, "claude", pre("Read", "file_path", ".env"), 0, `"permissionDecision":"deny"`},
		{"claude push", repo, "claude", pre("Bash", "command", "git push"), 0, `"permissionDecision":"deny"`},
		{"opencode deny", repo, "opencode", pre("Edit", "file_path", "src/a.go"), 0, `"permissionDecision":"deny"`},
		{"cursor deny", repo, "cursor", pre("Write", "file_path", "src/a.go"), 0, `"permission":"deny"`},
		{"cursor shell", repo, "cursor", `{"hook_event_name":"beforeShellExecution","command":"git push"}`, 0, `"permission":"deny"`},
		{"cursor ask", guided, "cursor", `{"hook_event_name":"beforeShellExecution","command":"git push"}`, 0, `"permission":"ask"`},
		{"cursor allow", repo, "cursor", pre("Write", "file_path", "docs/a.md"), 0, ""},
		{"gemini deny", repo, "gemini", pre("write_file", "file_path", "src/a.go"), 2, ""},
		{"gemini allow", repo, "gemini", pre("read_file", "file_path", "src/a.go"), 0, ""},
		{"unknown tool", repo, "claude", pre("Frobnicate", "file_path", "src/a.go"), 0, ""},
		{"unknown gemini tool", repo, "gemini", pre("frobnicate", "file_path", "src/a.go"), 0, ""},
		{"malformed", repo, "claude", `{"tool_name": `, 0, ""},
		{"not an object", repo, "claude", `[1,"x"]`, 0, ""},
		{"wrong types", repo, "claude", `{"tool_name":["Edit"],"tool_input":"x"}`, 0, ""},
		{"empty", repo, "cursor", ``, 0, ""},
	} {
		out, code := hook(t, c.repo, c.host, c.in)
		if code != c.code || c.out == "" && out != "" || c.out != "" && !strings.Contains(out, c.out) {
			t.Errorf("%s: exit %d out %q, want exit %d out %q", c.name, code, out, c.code, c.out)
		}
	}
}
