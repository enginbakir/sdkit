package setup

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestShared(t *testing.T) {
	s, f, a := New(t.TempDir()), ".claude/settings.json", "templates/blocks/AGENTS.kit.md"
	s.Out = io.Discard
	put(s.abs(f), []byte(`{"model":"opus","env":{"FOO":"bar"},"permissions":{"allow":["Bash(npm test)","Edit"],"ask":["Bash(curl *)"]},"hooks":{"Notification":[{"hooks":[{"type":"command","command":"notify-send hi"}]}],"Stop":[{"hooks":[{"type":"command","command":"echo mine"},{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/stop-verify.sh","timeout":5}]}]}}`), 0o644)
	s.InstallShared("claude-settings", f, "templates/settings.copy.json")
	s.InstallShared("claude-settings", f, "templates/settings.copy.json") // re-run: no diff
	b, _ := os.ReadFile(s.abs(f))
	testutil.Golden(t, "setup/settings", string(b))
	put(s.abs("AGENTS.md"), []byte("mine\n"), 0o644)
	s.ApplyShared("block-md", "AGENTS.md", a, false) // append, then replace
	s.ApplyShared("block-md", "AGENTS.md", a, false)
	if b, _ := os.ReadFile(s.abs("AGENTS.md")); s.ApplyShared("claude-settings", f, "templates/settings.copy.json", true) != nil || !strings.HasPrefix(string(b), "mine\n") || strings.Count(string(b), "spec-driven-kit:begin") != 1 {
		t.Errorf("not idempotent or block replace lost text:\n%s", b)
	}
}
