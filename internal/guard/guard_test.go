package guard

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/hook"
	"github.com/wecii/sdkit/internal/testutil"
)

func TestGuards(t *testing.T) {
	r := testutil.TempRepo(t)
	t.Setenv("CLAUDE_PROJECT_DIR", r)
	_ = os.Mkdir(filepath.Join(r, ".sdkit"), 0o755)
	for f, s := range map[string]string{".sdkit/sdkit.env": "STACK=none\nSECRET_GLOBS=.env*\nPROTECTED_PATHS=infra\nPROTECTED_BRANCHES=main\n", ".sdkit/.frozen": "a_test.go\n", ".env.local": "", "infra": "", "a_test.go": ""} {
		_ = os.WriteFile(filepath.Join(r, f), []byte(s), 0o644)
	}
	_ = exec.Command("git", "-C", r, "add", "-A").Run()
	var b bytes.Buffer
	PreCommit(r, &b)
	PrePush(r, strings.NewReader("refs/heads/main 1 refs/heads/main 0000\n"), &b)
	for _, cmd := range strings.Split("git push o f|env git push|command git push|nohup git push &|A=1 git push|x; git -C . push|ok && /usr/bin/git push|(git push)|gh api r/pulls -f a=b|git push --no-verify|bash .sdkit/scripts/sdd.sh _spawn a|sdkit sdd _spawn a|S=_evidence; sdkit sdd $S a|sdkit sdd _spawn-close a|sdkit sdd _verdict a|echo {} > .sdkit/attestations/x.json|git add .sdkit/attestations/x.json|git commit -m 'fix push retry'|ls", "|") {
		var o bytes.Buffer
		hook.Run("", "guard-pr", []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":`+strconv.Quote(cmd)+`}}`), &o, &o)
		b.WriteString("$ " + cmd + "\n" + o.String())
	}
	testutil.Golden(t, "guard/refusals", b.String())
}
