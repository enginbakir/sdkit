package runrole

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestDryRun(t *testing.T) {
	repo, g, re := testutil.TempRepo(t), strings.Builder{}, []*regexp.Regexp{regexp.MustCompile(`--output-last-message \S+`), regexp.MustCompile(`<prompt:\d+ chars>`)}
	os.WriteFile(repo+"/h.md", []byte("do the thing\n"), 0o644)
	for _, c := range []string{"claude|verifier|--disallowedTools Edit\\ Write\\ NotebookEdit|g", "claude|developer|--permission-mode acceptEdits|g", "codex|verifier|--sandbox read-only|g", "codex|developer|--sandbox workspace-write|g", "opencode|verifier|--agent verifier", "opencode|developer|--agent developer", "cursor|verifier|--mode ask", "cursor|developer|--force", "gemini|verifier|--approval-mode plan", "gemini|developer|--approval-mode auto_edit", "claude --tier small|developer|--model haiku", "codex --tier large|verifier|-c model_reasoning_effort=high", "opencode --tier small|developer|-m anthropic/claude-haiku-5-5"} {
		f, o := strings.Split(c, "|"), bytes.Buffer{}
		if rc := Run(append([]string{f[1], "--handoff", repo + "/h.md", "--cwd", repo, "--dry-run", "--host"}, strings.Fields(f[0])...), &o, &o); rc != 0 || !strings.Contains(o.String(), f[2]) || !strings.Contains(o.String(), shq(repo)) {
			t.Errorf("%s: rc=%d %s", c, rc, &o)
		}
		if len(f) == 4 {
			g.WriteString("$ run-role " + f[1] + " --host " + f[0] + " --dry-run\n" + strings.ReplaceAll(re[1].ReplaceAllString(re[0].ReplaceAllString(o.String(), "--output-last-message <OUT>"), "<prompt:N chars>"), shq(repo), "<CWD>"))
		}
	}
	testutil.Golden(t, "runrole/dryrun", g.String())
}
