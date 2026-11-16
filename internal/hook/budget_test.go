package hook

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/usage"
)

func TestBudgetOverSpawn(t *testing.T) {
	t.Chdir(t.TempDir())
	o, _ := &bytes.Buffer{}, errors.Join(state.Init(project.Resolve("."), "x", "", ""), usage.Log(usage.LedgerFile(project.Resolve("."), "x"), usage.Line{V: 2, ID: "i", In: 50}))
	for mode, want := range map[string]string{"strict": `"permissionDecision":"ask"`, "guided": `"systemMessage"`} {
		o.Reset()
		_ = os.WriteFile(".sdkit/sdkit.env", []byte("TOKEN_BUDGET_TASK=10\nSDD_MODE="+mode+"\n"), 0o644)
		if Run("", "guard-budget", []byte(`{"hook_event_name":"PreToolUse","tool_name":"Agent"}`), o, o); !strings.Contains(o.String(), want) || mode == "guided" && strings.Contains(o.String(), "permissionDecision") {
			t.Errorf("%s: %q", mode, o.String())
		}
	}
}
