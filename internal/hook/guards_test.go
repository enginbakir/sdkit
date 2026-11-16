package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestGuards(t *testing.T) {
	r := testutil.TempRepo(t)
	t.Setenv("CLAUDE_PROJECT_DIR", r)
	testutil.FakeCLI(t, "kubectl", "echo gke-prod")
	if o, err := exec.Command("sh", "-c", "cd '"+r+"' && git -c user.name=t -c user.email=t@t commit --allow-empty -qm i && git checkout -qb feat/x").CombinedOutput(); err != nil && os.PathSeparator == '/' {
		t.Fatalf("%s", o)
	}
	_ = os.MkdirAll(filepath.Join(r, ".sdkit"), 0o755)
	_ = os.WriteFile(filepath.Join(r, ".sdkit", ".frozen"), []byte("src/a_test.go\n"), 0o644)
	settings := func(s string) {
		_ = os.WriteFile(filepath.Join(r, ".sdkit", "sdkit.env"), []byte("STACK=none\n"+s+"\n"), 0o644)
	}
	run := func(g string, in []byte) string { var o bytes.Buffer; Run("", g, in, &o, &o); return o.String() }
	const rel, noisy, prot = "BASE_BRANCH_MODE=release\nPROTECTED_BRANCHES=staging,prod", `NOISY_CMD_PATTERNS="npm test*"`, "PROTECTED_PATHS=infra,.sdkit/.sdd"
	for _, c := range []struct{ g, tool, v, set, want string }{ // g is guard[:agent]; v is the path or the command
		{"guard-secrets", "Write", "{R}/cfg/.env.local", "SECRET_GLOBS=.env*", "deny"},
		{"guard-secrets", "Read", "main.go", "SECRET_GLOBS=.env*", "allow"},
		{"guard-secrets", "Read", ".env", "SECRET_GLOBS=", "allow"},
		{"guard-protected", "Edit", "{R}/docs/../infra/x", prot, "ask"},
		{"guard-protected", "Write", "./infra//x", prot, "ask"},
		{"guard-protected", "Write", "{R}/.sdkit/./.sdd/state.json", prot, "ask"},
		{"guard-protected", "Edit", "{R}/infrastructure/x", prot, "allow"},
		{"frozen-tests", "Edit", "{R}/src/a_test.go", "", "deny"},
		{"frozen-tests", "Write", "{R}/src/../src/a_test.go", "", "deny"},
		{"frozen-tests", "Edit", "{R}/src/b_test.go", "", "allow"},
		{"guard-readonly", "Write", "a", "", "deny"},
		{"guard-readonly", "Bash", "git status && git commit -am x", "", "deny"},
		{"guard-readonly", "Bash", "cd x; sudo rm y", "", "deny"},
		{"guard-readonly", "Bash", "env sudo rm y", "", "allow"}, // one prefix is stripped, as in the bash
		{"guard-readonly", "Bash", "sed -n 1p f", "", "deny"},    // kept from the bash: -[a-z]*i* matches any sed flag
		{"guard-readonly", "Bash", "echo x>out", "", "deny"},
		{"guard-readonly", "Bash", "git diff main...HEAD 2>&1 | tail -40", "", "allow"},
		{"guard-readonly:developer", "Write", "a", "", "allow"},
		{"guard-noisy", "Bash", "cd app && npm test", noisy, "ask"},
		{"guard-noisy", "Bash", "npm test 2>&1 | tail -20", noisy, "allow"},
		{"guard-noisy:developer", "Bash", "npm test", noisy, "allow"},
		{"guard-noisy", "Bash", "npm test", noisy + "\nSDD_MODE=off", "allow"},
		{"guard-prod", "Bash", "kubectl --context=x delete pod a", "PROD_KUBE_CLUSTERS=prod", "ask"},
		{"guard-prod", "Bash", "kubectl get pods", "PROD_KUBE_CLUSTERS=prod", "allow"},
		{"guard-git", "Bash", "git pull --no-rebase origin develop", rel, "deny"},
		{"guard-git", "Bash", "git merge developer-notes", rel, "allow"},
		{"guard-git", "Bash", "git checkout -b f origin/develop", rel, "deny"},
		{"guard-git", "Bash", "git push origin HEAD:prod", rel, "ask"},
		{"guard-git", "Bash", "git merge origin/develop", "", "allow"},
	} {
		settings(c.set)
		g, agent, _ := strings.Cut(c.g, ":")
		if g == "guard-readonly" && agent == "" {
			agent = "reviewer"
		}
		in, _ := json.Marshal(map[string]any{"hook_event_name": PreToolUse, "tool_name": c.tool, "agent_type": agent,
			"tool_input": map[string]string{"file_path": strings.ReplaceAll(c.v, "{R}", filepath.ToSlash(r)), "command": c.v}})
		got := "allow"
		for _, k := range []string{"deny", "ask"} {
			if strings.Contains(run(g, in), `"permissionDecision":"`+k+`"`) {
				got = k
			}
		}
		if got != c.want {
			t.Errorf("%s %s %q: got %s want %s", g, c.tool, c.v, got, c.want)
		}
	}
	cases, _ := os.ReadFile("../../testdata/golden/hook/guards.txt")
	settings("SECRET_GLOBS=.env*\nPROD_KUBE_CLUSTERS=prod\n" + noisy)
	var got strings.Builder
	for _, l := range strings.Split(string(cases), "\n") {
		if f := strings.SplitN(strings.TrimPrefix(l, "# in: "), "|", 3); len(f) == 3 && strings.HasPrefix(l, "# in: ") {
			var v any
			_ = json.Unmarshal([]byte(run(f[1], []byte(f[2]))), &v)
			b := &bytes.Buffer{}
			e := json.NewEncoder(b)
			e.SetEscapeHTML(false)
			_ = e.Encode(v)
			got.WriteString("== " + f[0] + "\nstdout: " + b.String())
		}
	}
	testutil.Golden(t, "hook/guards", got.String())
}
