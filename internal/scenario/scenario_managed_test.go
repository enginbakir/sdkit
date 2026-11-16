package scenario

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/jira/jiratest"
	"github.com/wecii/sdkit/internal/testutil"
)

func task(sha string) jiratest.Issue {
	return jiratest.Issue{Status: "To Do", Category: "new", Parent: "SDK-1", Desc: "```sdkit-task\n" + fmt.Sprintf(`{"specId":"s1","externalId":"s1:T1","specVersion":1,"specSha":%q,"acceptanceCriteria":["AC1"],"risk":"low","dependencies":[],"scope":["src/**"],"parent":"SDK-1"}`, sha) + "\n```"}
}

// D: managed mode with a fake Jira and a specs checkout: contract, pinned revision, spec-change block, revalidate, idempotent evidence-sync.
func TestScenarioD(t *testing.T) {
	sp, origin := t.TempDir(), t.TempDir() // approved spec v1 (sha[0]) and v2 (sha[1]) on origin/main
	git(t, origin, "init", "-q", "--bare")
	git(t, sp, "init", "-q", "-b", "main")
	git(t, sp, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, sp, "remote", "add", "origin", origin)
	var sha [2]string
	for i := range sha {
		write(t, sp, "specs/s1/spec.meta.json", fmt.Sprintf(`{"status":"approved","version":%d}`, i+1))
		git(t, sp, "add", "-A")
		git(t, sp, "commit", "-qm", "spec")
		c := exec.Command("git", "rev-parse", "HEAD")
		c.Dir = sp
		out, _ := c.Output()
		sha[i] = strings.TrimSpace(string(out))
	}
	git(t, sp, "push", "-q", "origin", "main")
	git(t, sp, "fetch", "-q", "origin")
	srv := jiratest.New(t, map[string]jiratest.Issue{"SDK-5": task(sha[0]), "SDK-6": {Desc: "no contract", Status: "To Do"}})
	t.Setenv("JIRA_API_TOKEN", "tok")
	repo := kit(t, "SDD_MANAGED=on\nVERIFY_CMD=true\nPR_HOST=github\nJIRA_EGRESS=allow\nJIRA_BASE_URL="+srv.URL+"\nJIRA_USER=u\nJIRA_REVIEW_STATUS=\"In Progress\"\nSPECS_DIR=specs\n")
	if err := os.Rename(filepath.Join(sp, "specs"), filepath.Join(repo, "specs")); err != nil || os.Rename(filepath.Join(sp, ".git"), filepath.Join(repo, "specs", ".git")) != nil {
		t.Fatal("move specs")
	}
	write(t, repo, ".gitignore", ".sdkit/.sdd/\n.sdkit/audit.jsonl\nspecs/\n")
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	git(t, repo, "remote", "add", "origin", remote)
	sd(t, repo, 1, "sdd", "start", "--ticket", "SDK-6") // contract invalid
	sd(t, repo, 1, "sdd", "start", "--ticket", "SDK-9") // not found
	sd(t, repo, 0, "sdd", "start", "--ticket", "SDK-5")
	gate(t, repo, pre("Edit", "file_path", "specs/s1/spec.meta.json"), true) // read-only specs
	sd(t, repo, 0, "sdd", "advance", "proposed")
	write(t, repo, "a_test.go", "package a\n")
	sd(t, repo, 0, "sdd", "advance", "red", "--test-cmd", "test -f impl.txt")
	write(t, repo, "impl.txt", "1")
	write(t, repo, "pr.md", "body")
	sd(t, repo, 0, "sdd", "advance", "green")
	sd(t, repo, 0, "sdd", "advance", "verified")
	gh := filepath.Join(t.TempDir(), "gh.log")
	testutil.FakeCLI(t, "gh", `echo "$@" >> `+gh+`; case "$1$2" in prlist) echo '[]';; *) echo https://example.test/pr/1;; esac`)
	git(t, repo, "push", "-q", "origin", "main", "feat")
	has(t, sd(t, repo, 0, "pr", "open", "--reuse", "T", "pr.md", "main"), "https://example.test/pr/1")
	if b, _ := os.ReadFile(gh); !strings.Contains(string(b), "pr create --base main --head feat") {
		t.Fatalf("gh args: %s", b)
	}
	sd(t, repo, 0, "sdd", "advance", "pr")
	sd(t, repo, 0, "sdd", "evidence-sync", "--pr", "https://example.test/pr/1")
	has(t, sd(t, repo, 0, "sdd", "evidence-sync", "--pr", "https://example.test/pr/1"), "nothing sent")
	sd(t, repo, 0, "sdd", "block", "--spec-change", "--reason", "AC1 is wrong", "--section", "ACs")
	sd(t, repo, 1, "sdd", "advance", "done")
	sd(t, repo, 1, "sdd", "evidence-sync")
	gate(t, repo, pre("Edit", "file_path", "src/a.go"), true)
	gate(t, repo, pre("Bash", "command", "git push"), true)
	sd(t, repo, 1, "sdd", "revalidate", "--sha", sha[0])                      // not a changed revision
	sd(t, repo, 1, "sdd", "revalidate", "--sha", sha[1])                      // Jira still pins the old one
	next := jiratest.New(t, map[string]jiratest.Issue{"SDK-5": task(sha[1])}) // the re-pinned subtask, on a second fake
	env, _ := os.ReadFile(filepath.Join(repo, ".sdkit/sdkit.env"))
	write(t, repo, ".sdkit/sdkit.env", strings.ReplaceAll(string(env), srv.URL, next.URL))
	sd(t, repo, 0, "sdd", "revalidate", "--sha", sha[1])
	has(t, sd(t, repo, 0, "sdd", "status"), "phase=proposed")
	sd(t, repo, 1, "sdd", "revalidate", "--sha", sha[1]) // no longer blocked
}
