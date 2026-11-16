package hook

import (
	"bytes"
	"testing"
)

func TestStopVerifyHonoursStopHookActive(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	var out, errb bytes.Buffer
	if Run("", "stop-verify", []byte(`{"stop_hook_active":true}`), &out, &errb) != 0 || out.Len() != 0 {
		t.Fatalf("want silence, got %q", out.String())
	}
}
