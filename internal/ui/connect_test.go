package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	good := ConnectionInput{JiraBaseURL: "https://x.atlassian.net", JiraProject: "TFF", JiraUser: "a@b.co"}
	cases := []struct {
		name   string
		mutate func(*ConnectionInput)
		bad    bool
	}{
		{"complete input", func(*ConnectionInput) {}, false},
		{"http is refused: a token must not cross the network in clear", func(i *ConnectionInput) { i.JiraBaseURL = "http://x.atlassian.net" }, true},
		{"address without a host", func(i *ConnectionInput) { i.JiraBaseURL = "https://" }, true},
		{"lowercase project key", func(i *ConnectionInput) { i.JiraProject = "tff" }, true},
		{"user is not an e-mail", func(i *ConnectionInput) { i.JiraUser = "ahmet" }, true},
		{"empty tokens keep the saved ones", func(i *ConnectionInput) { i.JiraToken, i.BitbucketToken = "", "" }, false},
		{"token with a pasted space", func(i *ConnectionInput) { i.JiraToken = "ATATT3xFfGF0 abcdef" }, true},
		{"token with a dollar the reader would expand", func(i *ConnectionInput) { i.JiraToken = "abcd$HOME1234" }, true},
		{"real looking token", func(i *ConnectionInput) { i.JiraToken = "ATATT3xFfGF0_abc-DEF=12345678" }, false},
		{"bitbucket e-mail given but wrong", func(i *ConnectionInput) { i.BitbucketEmail = "nope" }, true},
	}
	for _, c := range cases {
		in := good
		c.mutate(&in)
		if err := in.Validate(); (err != nil) != c.bad {
			t.Errorf("%s: err = %v, want bad=%v", c.name, err, c.bad)
		}
	}
}

func TestSetEnvKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sdkit.local.env")
	os.WriteFile(p, []byte("# mine\nMAX_PARALLEL=3\nJIRA_USER=old@x.co\n# JIRA_PROJECT=commented\n"), 0o644)
	if err := setEnvKeys(p, []envKV{{"JIRA_USER", "new@x.co"}, {"JIRA_PROJECT", "TFF"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	for _, want := range []string{"# mine\n", "MAX_PARALLEL=3\n", "JIRA_USER=new@x.co\n", "# JIRA_PROJECT=commented\n", "JIRA_PROJECT=TFF\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old@x.co") {
		t.Error("the old value must be replaced in place, not kept")
	}
	if st, _ := os.Stat(p); os.PathSeparator == '/' && st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	if err := setEnvKeys(p, []envKV{{"X", "a\nb"}}); err == nil {
		t.Error("a value with a newline must be refused")
	}
}

func TestSaveConnectionRefusesAFileGitWouldCommit(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	os.MkdirAll(filepath.Join(root, ".sdkit"), 0o755)
	os.WriteFile(filepath.Join(root, ".sdkit", "sdkit.env"), []byte("SDKIT_ENV=analyst\n"), 0o644)
	in := ConnectionInput{JiraBaseURL: "https://x.atlassian.net", JiraProject: "TFF", JiraUser: "a@b.co", JiraToken: "ATATT3xFfGF0_abc-DEF=12345678"}

	if err := SaveConnection(root, in); err == nil {
		t.Fatal("a token must not be written to a file git does not ignore")
	}
	if _, err := os.Stat(filepath.Join(root, ".sdkit", "sdkit.local.env")); err == nil {
		t.Fatal("the refusal must not leave the file behind")
	}
	os.WriteFile(filepath.Join(root, ".sdkit", ".gitignore"), []byte("sdkit.local.env\n"), 0o644)
	if err := SaveConnection(root, in); err != nil {
		t.Fatal(err)
	}
	c := ReadConnection(root)
	if c.JiraUser != "a@b.co" || !c.JiraTokenSet || c.JiraBaseURL != "https://x.atlassian.net" {
		t.Errorf("read back %+v", c)
	}
	shared, _ := os.ReadFile(filepath.Join(root, ".sdkit", "sdkit.env"))
	if strings.Contains(string(shared), "ATATT") || strings.Contains(string(shared), "a@b.co") {
		t.Error("nothing personal may reach the shared, committed file")
	}
	in.JiraToken = ""
	if err := SaveConnection(root, in); err != nil || !ReadConnection(root).JiraTokenSet {
		t.Error("saving again without a token must keep the saved one")
	}
}

func TestChecks(t *testing.T) {
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/myself") && r.Header.Get("Authorization") == "" {
			t.Error("the Jira check must send credentials")
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"displayName":"Ayşe Analist"}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	cases := []struct {
		status int
		ok     bool
		want   string
	}{{200, true, "Ayşe Analist"}, {401, false, "kabul etmedi"}, {404, false, "bulunamadı"}, {500, false, "HTTP 500"}}
	for _, c := range cases {
		status = c.status
		got := CheckJira(ctx, srv.Client(), srv.URL, "a@b.co", "tok")
		if got.OK != c.ok || !strings.Contains(got.Who+got.Message, c.want) {
			t.Errorf("jira %d: %+v", c.status, got)
		}
	}
	if got := CheckJira(ctx, srv.Client(), "http://127.0.0.1:1", "a@b.co", "tok"); got.OK || !strings.Contains(got.Message, "ulaşılamadı") {
		t.Errorf("unreachable jira: %+v", got)
	}

	status = 200
	origin := "https://me@bitbucket.org/deloittetr/tff-specs.git"
	if got := CheckBitbucket(ctx, srv.Client(), srv.URL, origin, "a@b.co", "tok"); !got.OK || got.Who != "deloittetr/tff-specs" {
		t.Errorf("bitbucket ok: %+v", got)
	}
	if got := CheckBitbucket(ctx, srv.Client(), srv.URL, "https://github.com/x/y.git", "a@b.co", "tok"); got.OK {
		t.Error("an origin that is not Bitbucket must not pass")
	}
}

func TestParseDoctor(t *testing.T) {
	out := `doctor: /r
  ok    git repository
  FAIL  project.md still has TODO placeholders
  warn  no server-side gate in CI: local hooks can be skipped with --no-verify
  ok    openspec CLI installed

setup checklist (Jira and pull requests)
  TODO 1. JIRA_API_TOKEN (your Jira API token)
          .sdkit/sdkit.env: JIRA_API_TOKEN=<token>
  TODO 2. JIRA_EGRESS is not allow: sdkit jira will not call Jira
  TODO 3. set a Bitbucket API token
doctor: 1 failure(s), 3 warning(s)`
	items := ParseDoctor(out)
	var main []string
	for _, it := range items {
		if it.Main {
			main = append(main, it.Level+":"+it.Title)
		}
	}
	want := []string{"problem:Proje profili (project.md) doldurulmamış", "ok:OpenSpec kurulu", "todo:Jira bağlantısı eksik veya çalışmıyor", "todo:Bitbucket bağlantısı eksik veya çalışmıyor"}
	if strings.Join(main, "|") != strings.Join(want, "|") {
		t.Errorf("main items:\n got %v\nwant %v", main, want)
	}
	for _, it := range items {
		if strings.Contains(it.Raw, "server-side gate") && (it.Main || it.Level != LevelNote) {
			t.Errorf("the CI warning is a developer topic, got %+v", it)
		}
		if it.Fix == FixConnect && it.Level != LevelTodo {
			t.Errorf("a Connect fix must be a todo: %+v", it)
		}
	}
	if Ready(items) {
		t.Error("open problems and todos mean not ready")
	}
	if !Ready(ParseDoctor("  ok    git repository\n  warn  something minor")) {
		t.Error("only ok lines and notes means ready")
	}
}
