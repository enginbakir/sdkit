package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func read(t *testing.T, repo, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// E: init, sync and upgrade never overwrite project-owned files; settings are only appended;
// doctor reports the right FAILs; per-host files for all five hosts are generated.
func TestScenarioE(t *testing.T) {
	t.Parallel()
	repo := testutil.TempRepo(t)
	sd(t, repo, 0, "init", "--host", "claude,codex,opencode,cursor,gemini", "--stack", "go")
	for _, f := range []string{"AGENTS.md", ".claude/CLAUDE.md", ".claude/settings.local.json", ".codex/config.toml", ".opencode/plugins",
		".cursor/hooks.json", ".gemini/settings.json", ".agents/skills/spec-driven-dev/SKILL.md", ".sdkit/project.md", ".sdkit/rules/git.md", ".githooks/pre-push"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Errorf("init did not create %s", f)
		}
	}
	for _, d := range []string{".codex", ".opencode/agents", ".cursor/agents", ".gemini/agents"} {
		if m, _ := filepath.Glob(filepath.Join(repo, d, "*")); len(m) == 0 {
			t.Errorf("no generated files in %s", d)
		}
	}
	out := sd(t, repo, 1, "doctor")
	has(t, out, "FAIL  project.md still has TODO placeholders")
	has(t, out, "FAIL  BASE_BRANCH 'main' is not a branch")

	// Project-owned edits, a dropped setting, a user key in a shared file and a locally edited managed copy.
	const skill = ".agents/skills/spec-driven-dev/SKILL.md"
	env := strings.Replace(read(t, repo, ".sdkit/sdkit.env"), "SDD_MODE=strict\n", "", 1)
	mine := map[string]string{".sdkit/project.md": "MINE\n", ".sdkit/rules/git.md": "MINE\n", ".sdkit/sdkit.env": env + "MY_KEY=1\n",
		".gemini/settings.json": `{"x":1}`, skill: read(t, repo, skill) + "LOCAL\n"}
	for f, body := range mine {
		write(t, repo, f, body)
	}
	if out := sd(t, repo, 1, "sync", "--check"); !strings.Contains(out, "drift") {
		t.Errorf("sync --check saw no drift: %s", out)
	}
	for _, args := range [][]string{{"sync"}, {"upgrade"}, {"upgrade"}} {
		sd(t, repo, 0, args...)
		for _, f := range []string{".sdkit/project.md", ".sdkit/rules/git.md"} {
			if read(t, repo, f) != "MINE\n" {
				t.Fatalf("%s overwritten by %v", f, args)
			}
		}
		if got := read(t, repo, ".sdkit/sdkit.env"); !strings.Contains(got, "MY_KEY=1\n") || strings.Count(got, "SDD_MODE=strict\n") > 1 {
			t.Fatalf("sdkit.env after %v: user edit lost or setting duplicated", args)
		}
		if g := read(t, repo, ".gemini/settings.json"); !strings.Contains(g, `"x": 1`) || !strings.Contains(g, "sdkit hook --host gemini") {
			t.Fatalf("gemini settings after %v: %s", args, g)
		}
		// Upgrade re-runs init, which re-records the edited copy's hash, so only sync is held to this (same as 1.x).
		if args[0] == "sync" && !strings.HasSuffix(read(t, repo, skill), "LOCAL\n") {
			t.Fatalf("local edit of %s clobbered without --force", skill)
		}
	}
	if !strings.Contains(read(t, repo, ".sdkit/sdkit.env"), "SDD_MODE=strict\n") {
		t.Error("upgrade did not append the dropped setting")
	}
	if out, code := testutil.RunKit(t, repo, "", "init", "--host", "claude", "--stack", "go", "--force"); code != 0 || read(t, repo, ".sdkit/project.md") != "MINE\n" {
		t.Errorf("init --force (exit %d) touched project.md: %s", code, out)
	}
}
