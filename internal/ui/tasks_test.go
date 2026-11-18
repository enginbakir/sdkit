package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/spec"
)

func TestGenerateTasksTask(t *testing.T) {
	sha := strings.Repeat("a", 40)
	cases := []struct {
		name string
		in   TaskInput
		ok   bool
	}{
		{"parent and full sha", TaskInput{Parent: "TFF-12", SHA: sha}, true},
		{"lower-case key", TaskInput{Parent: "tff-12", SHA: sha}, false},
		{"key with a JQL tail", TaskInput{Parent: "TFF-12 OR 1=1", SHA: sha}, false},
		{"short sha", TaskInput{Parent: "TFF-12", SHA: "abc1234"}, false},
		{"branch name instead of sha", TaskInput{Parent: "TFF-12", SHA: "main"}, false},
		{"no parent", TaskInput{Parent: "", SHA: sha}, false},
	}
	for _, c := range cases {
		got, err := Task("", "generate-tasks", "odeme-akisi", c.in)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if c.ok && (!strings.Contains(got, "TFF-12") || !strings.Contains(got, sha) || !strings.Contains(got, "generate-tasks")) {
			t.Errorf("%s: task = %q", c.name, got)
		}
	}
}

func TestCheckParent(t *testing.T) {
	cases := []struct {
		name   string
		status int
		key    string
		ok     bool
		msg    string
	}{
		{"exists", 200, "TFF-12", true, ""},
		{"unknown to Jira", 404, "TFF-99", false, "bulunamadı"},
		{"no permission", 403, "TFF-12", false, "izin"},
		{"not a key at all", 200, "../x", false, "TFF-123"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(`{"fields":{"summary":"Ödeme","issuetype":{"name":"Epic"},"status":{"name":"To Do"}}}`))
		}))
		got := CheckParent(context.Background(), srv.Client(), srv.URL, "a@b.c", "tok", c.key)
		srv.Close()
		if got.OK != c.ok || (c.msg != "" && !strings.Contains(got.Message, c.msg)) {
			t.Errorf("%s: %+v", c.name, got)
		}
		if c.ok && (got.Summary != "Ödeme" || got.Type != "Epic") {
			t.Errorf("%s: the page shows what Jira says: %+v", c.name, got)
		}
	}
}

func TestPlanBounds(t *testing.T) {
	tasks := make([]PlanTask, 80)
	for i := range tasks {
		tasks[i] = PlanTask{Ref: "T", Title: strings.Repeat("x", 900), ACs: make([]string, 100)}
	}
	raw, _ := json.Marshal(map[string]any{"questions": []QuestionItem{
		{Question: "Onay?", Plan: &Plan{Summary: "ok", Tasks: tasks}},
		{Question: "Boş plan", Plan: &Plan{Summary: "x"}},
	}})
	out, err := NormalizeQuestions(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Questions []QuestionItem }
	json.Unmarshal(out, &got)
	if p := got.Questions[0].Plan; p == nil || len(p.Tasks) != 60 || len(p.Tasks[0].ACs) != 30 || len([]rune(p.Tasks[0].Title)) > 301 {
		t.Errorf("plan not bounded: %+v", p)
	}
	if got.Questions[1].Plan != nil {
		t.Error("a plan with no tasks is just the question")
	}
}

func TestApprovedCommitsAndResult(t *testing.T) {
	root := scratchRepo(t, "/prespec/\n/.sdkit/work/\n")
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	meta := filepath.Join(root, "specs", "odeme", "spec.meta.json")
	os.MkdirAll(filepath.Dir(meta), 0o755)
	for _, v := range [][2]string{{"draft", "0.1.0"}, {"approved", "1.0.0"}, {"approved", "1.1.0"}} {
		st := v[0]
		os.WriteFile(meta, []byte(`{"status":"`+st+`","version":"`+v[1]+`"}`), 0o644)
		git("add", "-A")
		git("commit", "-q", "-m", "spec "+st)
	}
	if got := spec.ApprovedCommits(root, "odeme", 30); len(got) != 0 {
		t.Fatalf("without origin/main nothing is offered: %v", got)
	}
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	got := spec.ApprovedCommits(root, "odeme", 30)
	if len(got) != 2 {
		t.Fatalf("the draft commit is not an approval, the two approved ones are: %+v", got)
	}
	if got[0].Version != "1.1.0" || got[0].SHA == got[1].SHA || len(got[0].SHA) != 40 {
		t.Errorf("full distinct SHAs expected: %+v", got)
	}
	if spec.ApprovedCommits(root, "../x", 30) != nil {
		t.Error("an id that is not a spec name is refused")
	}

	dir := filepath.Join(root, ".sdkit", "work", "tasks", "odeme")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "results.jsonl"), []byte(`{"key":"TFF-31","action":"created"}`+"\n"+`{"key":"junk"}`+"\n"+`{"key":"TFF-32","action":"updated"}`+"\n"), 0o644)
	r := ReadTaskResult(root, "odeme", "https://x.atlassian.net/")
	if len(r.Tasks) != 2 || r.Tasks[0].URL != "https://x.atlassian.net/browse/TFF-31" || r.Done {
		t.Errorf("links from results, not done without a report: %+v", r)
	}
	os.WriteFile(filepath.Join(dir, "report.md"), []byte("# report\n| T1 | TFF-31 | created |"), 0o644)
	if r := ReadTaskResult(root, "odeme", ""); !r.Done || r.Tasks[0].URL != "" {
		t.Errorf("done with a clean report: %+v", r)
	}
	os.WriteFile(filepath.Join(dir, "report.md"), []byte("T2 | NOT CREATED"), 0o644)
	if ReadTaskResult(root, "odeme", "").Done {
		t.Error("a report that lists a task NOT CREATED is not a success")
	}
}
