package secretscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func git(t *testing.T, d string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", d, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, d, name, body string) {
	t.Helper()
	p := filepath.Join(d, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rules(fs []Finding) (ids []string) {
	for _, f := range fs {
		ids = append(ids, f.Rule+"@"+f.Path)
	}
	return
}

func want(t *testing.T, what string, got []Finding, ids ...string) {
	t.Helper()
	if g, w := strings.Join(rules(got), ","), strings.Join(ids, ","); g != w {
		t.Errorf("%s: got [%s], want [%s]", what, g, w)
	}
}

// One repository, four views: the tree, the staged change, the history, and a baseline over them.
func TestSources(t *testing.T) {
	d := testutil.TempRepo(t)
	stripe := "sk_li" + "ve_" + strings.Repeat("a", 24)
	aws := "AKIA" + strings.Repeat("B", 16)
	npm := "npm" + "_" + strings.Repeat("c", 36)

	write(t, d, "cfg/app.yml", "name: app\nkey: "+stripe+"\n")
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "add config")
	write(t, d, "cfg/app.yml", "name: app\nkey: ${STRIPE_KEY}\n")
	git(t, d, "commit", "-qam", "use env")
	first := strings.TrimSpace(gitOut(t, d, "rev-parse", "HEAD~1"))

	write(t, d, ".gitignore", "ignored.env\n")
	write(t, d, "ignored.env", "k="+aws+"\n")
	write(t, d, "local.env", "# first line\nk="+aws+"\n")
	write(t, d, "bin.dat", "\x00"+aws)

	tree, err := Scan(Options{Root: d})
	if err != nil {
		t.Fatal(err)
	}
	want(t, "tree: untracked file counts, ignored and binary do not, a removed secret is gone", tree, "aws-key-id@local.env")
	if tree[0].Line != 2 || tree[0].Source != SourceTree {
		t.Errorf("tree finding: %+v", tree[0])
	}
	testutil.Golden(t, "scan/findings", string(Report{Findings: tree}.JSON()))

	hist, _ := Scan(Options{Root: d, History: true})
	want(t, "history: a secret deleted later is still found", hist, "stripe-key@cfg/app.yml")
	if hist[0].Line != 2 || len(hist[0].Commit) != 12 || !strings.HasPrefix(first, hist[0].Commit) {
		t.Errorf("history finding: %+v (first commit %s)", hist[0], first)
	}
	if g, _ := Scan(Options{Root: d, History: true, Since: "HEAD"}); len(g) != 0 {
		t.Errorf("since HEAD must see nothing, got %v", rules(g))
	}
	if _, err := Scan(Options{Root: d, History: true, Since: "--all"}); err == nil {
		t.Error("a --since that is not a commit must be refused")
	}

	write(t, d, "ci.yml", "token: "+npm+"\n")
	git(t, d, "add", "ci.yml")
	st, _ := Scan(Options{Root: d, Staged: true})
	want(t, "staged", st, "npm-token@ci.yml")

	// A baseline entry follows the secret, not its line: moving it down a line keeps it accepted (the staged npm token is in the tree now, still open).
	bl := Baseline{Entries: []Entry{{Fingerprint: tree[0].Fingerprint, Reason: "test fixture"}}}
	write(t, d, "local.env", "# one\n# two\nk="+aws+"\n")
	moved, _ := Scan(Options{Root: d})
	if open, acc := bl.Split(moved); strings.Join(rules(open), ",") != "npm-token@ci.yml" || len(acc) != 1 || acc[0].Line != 3 {
		t.Errorf("baseline after a line move: open %v accepted %v", rules(open), rules(acc))
	}
}

func TestBaselineFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, BaselineFile)
	if b, err := LoadBaseline(p); err != nil || len(b.Entries) != 0 {
		t.Fatalf("a missing baseline is empty, got %v %v", b, err)
	}
	for _, bad := range []string{`{"entries":[{"fingerprint":"x"}]}`, `{not json`} {
		os.WriteFile(p, []byte(bad), 0o644)
		if _, err := LoadBaseline(p); err == nil {
			t.Errorf("%s must be an error, not an empty baseline", bad)
		}
	}
}

func gitOut(t *testing.T, d string, args ...string) string {
	t.Helper()
	b, err := exec.Command("git", append([]string{"-C", d}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
