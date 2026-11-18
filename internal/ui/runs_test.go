package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// panel starts a server over the repo with a stand-in host that stays busy for a few seconds, and returns a way to call it.
func panel(t *testing.T, root string, max int) (*Server, func(method, path, body string) (int, string)) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in host is a shell script")
	}
	// the stand-in host stays busy until the test says so, so nothing depends on how fast the machine is
	dir := t.TempDir()
	host, release := filepath.Join(dir, "host.sh"), filepath.Join(dir, "release")
	os.WriteFile(host, []byte("#!/bin/sh\nwhile [ ! -f '"+release+"' ]; do sleep 0.1; done\necho '{\"type\":\"result\",\"is_error\":false}'\n"), 0o755)
	srv := New(Config{Dir: root, Command: host, MaxRuns: max}, "k")
	l, _ := srv.Listen()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		os.WriteFile(release, nil, 0o644)
		srv.mu.Lock()
		for _, r := range srv.runs {
			r.cancel()
		}
		srv.mu.Unlock()
		cancel()
		time.Sleep(200 * time.Millisecond)
	})
	go srv.Serve(ctx, l)
	return srv, func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, "http://"+l.Addr().String()+path, strings.NewReader(body))
		req.Header.Set("X-Sdkit-Key", "k")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
}

func TestSpecsRunAtTheSameTimeEachInItsOwnTree(t *testing.T) {
	root := mainWithRemote(t)
	for _, id := range []string{"a", "b", "c"} {
		WritePrespec(root, id, "metin", false)
	}
	_, call := panel(t, root, 2)

	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"a"}`); code != http.StatusAccepted {
		t.Fatalf("a: %d %s", code, body)
	}
	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"b"}`); code != http.StatusAccepted {
		t.Fatalf("b runs while a does: %d %s", code, body)
	}
	if code, _ := call("POST", "/api/run", `{"action":"generate-specs","id":"a"}`); code != http.StatusConflict {
		t.Errorf("a spec has one run at a time: %d", code)
	}
	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"c"}`); code != http.StatusConflict || !strings.Contains(body, "En fazla 2") {
		t.Errorf("the cap holds: %d %s", code, body)
	}
	wts := Worktrees(root)
	if len(wts) != 2 || wts["a"] == "" || wts["b"] == "" || wts["a"] == wts["b"] {
		t.Errorf("each spec has a tree of its own, and c (refused) has none: %v", wts)
	}
	if Branch(root) == "spec/a" || Branch(root) == "spec/b" {
		t.Errorf("the main tree is untouched: %s", Branch(root))
	}
	var list struct {
		Runs []struct{ ID string }
		Max  int
	}
	_, body := call("GET", "/api/runs", "")
	json.Unmarshal([]byte(body), &list)
	if len(list.Runs) != 2 || list.Max != 2 {
		t.Errorf("runs list: %s", body)
	}

	call("POST", "/api/stop", `{"id":"a"}`)
	// the slot frees when the process tree is gone, which takes a moment on a busy machine
	for i := 0; i < 100; i++ {
		_, body = call("GET", "/api/runs", "")
		list.Runs = nil
		json.Unmarshal([]byte(body), &list)
		if len(list.Runs) <= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(list.Runs) != 1 || list.Runs[0].ID != "b" {
		t.Errorf("stopping a leaves b going: %s", body)
	}
	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"c"}`); code != http.StatusAccepted {
		t.Errorf("a freed slot is usable: %d %s", code, body)
	}
}

func TestOnlyOneSpecWorksInTheMainTree(t *testing.T) {
	root := mainWithRemote(t)
	for _, id := range []string{"m", "n"} { // both have drafts in the main tree: neither can be moved to a fresh checkout
		os.MkdirAll(filepath.Join(root, ".sdkit", "work", "spec-draft", id), 0o755)
	}
	_, call := panel(t, root, 3)
	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"m"}`); code != http.StatusAccepted {
		t.Fatalf("m: %d %s", code, body)
	}
	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"n"}`); code != http.StatusConflict || !strings.Contains(body, "Ana çalışma alanı") {
		t.Errorf("the main tree has one branch: %d %s", code, body)
	}
	if code, _ := call("POST", "/api/run", `{"action":"update-specs","id":"m"}`); code != http.StatusConflict {
		t.Errorf("and m has its one run: %d", code)
	}
}

func TestQuestionsBelongToTheSpecThatAsked(t *testing.T) {
	root := scratchRepo(t, "")
	srv, call := panel(t, root, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range []string{"a", "b"} {
		go srv.broker.Ask(ctx, json.RawMessage(`{"questions":[{"question":"Soru `+id+`?"}]}`), id)
	}
	time.Sleep(300 * time.Millisecond)
	for id, want := range map[string]string{"a": "Soru a?", "b": "Soru b?"} {
		_, body := call("GET", "/api/questions?id="+id, "")
		var qs []Question
		json.Unmarshal([]byte(body), &qs)
		if len(qs) != 1 || qs[0].Spec != id || !strings.Contains(string(qs[0].Questions), want) {
			t.Errorf("questions of %s: %s", id, body)
		}
	}
	if _, body := call("GET", "/api/questions", ""); strings.Count(body, `"spec"`) != 2 {
		t.Errorf("without an id, all of them: %s", body)
	}
}

// A question that never reaches the form must not look like one that was asked: the agent is told why, and so is the person.
func TestRefusedQuestionIsShownToThePerson(t *testing.T) {
	root := scratchRepo(t, "")
	srv, _ := panel(t, root, 3)
	l := srv.logFor("odeme")
	h := srv.Handler()
	req := httptest.NewRequest("POST", "/api/ask", strings.NewReader(`{"questions":[{"question":"   "}]}`))
	req.Host, req.Header = srv.addr, http.Header{"X-Sdkit-Key": {"k"}, "X-Sdkit-Spec": {"odeme"}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("the agent is told: %d", rec.Code)
	}
	past, _, cancel := l.Since(0)
	cancel()
	if len(past) != 1 || past[0].Name != "ask-refused" || past[0].Text == "" {
		t.Errorf("the person is told too: %+v", past)
	}
}

// postAsk sends a question the way `sdkit ui ask` does and stays waiting for the answer until the test ends.
func postAsk(t *testing.T, addr, spec string) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/api/ask", strings.NewReader(`{"questions":[{"question":"Risk ölçütü?","options":[{"label":"Yaz"}]}]}`))
	req.Header.Set("X-Sdkit-Key", "k")
	if spec != "" {
		req.Header.Set("X-Sdkit-Spec", spec)
	}
	go http.DefaultClient.Do(req)
	time.Sleep(300 * time.Millisecond)
	return cancel
}

func TestAQuestionWithoutAnOwnerIsStillSeen(t *testing.T) {
	root := mainWithRemote(t)
	for _, id := range []string{"a", "b"} {
		WritePrespec(root, id, "metin", false)
	}
	srv, call := panel(t, root, 3)
	addr := srv.addr

	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"a"}`); code != http.StatusAccepted {
		t.Fatalf("a: %d %s", code, body)
	}
	defer postAsk(t, addr, "")() // an older sdkit does not say which spec asks
	var qs []Question
	_, body := call("GET", "/api/questions?id=a", "")
	json.Unmarshal([]byte(body), &qs)
	if len(qs) != 1 || qs[0].Spec != "a" {
		t.Errorf("with one run going the question is that run's: %s", body)
	}

	if code, body := call("POST", "/api/run", `{"action":"generate-specs","id":"b"}`); code != http.StatusAccepted {
		t.Fatalf("b: %d %s", code, body)
	}
	defer postAsk(t, addr, "")()
	for _, id := range []string{"a", "b"} {
		_, body := call("GET", "/api/questions?id="+id, "")
		if !strings.Contains(body, `"id":2`) {
			t.Errorf("with two runs an unowned question is shown on both pages, not on neither: %s", body)
		}
	}
	var list struct{ Loose int }
	_, body = call("GET", "/api/runs", "")
	json.Unmarshal([]byte(body), &list)
	if list.Loose != 1 {
		t.Errorf("the page is told how many are unowned: %s", body)
	}
}

func TestTheSessionGetsLongBashTimeouts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in host is a shell script")
	}
	root := scratchRepo(t, "")
	os.MkdirAll(filepath.Join(root, ".sdkit", "work", "spec-draft", "odeme"), 0o755)
	host := filepath.Join(t.TempDir(), "host.sh")
	script := "#!/bin/sh\necho \"{\\\"type\\\":\\\"assistant\\\",\\\"message\\\":{\\\"content\\\":[{\\\"type\\\":\\\"text\\\",\\\"text\\\":\\\"T=$BASH_DEFAULT_TIMEOUT_MS/$BASH_MAX_TIMEOUT_MS\\\"}]}}\"\necho '{\"type\":\"result\",\"is_error\":false}'\n"
	os.WriteFile(host, []byte(script), 0o755)
	srv := New(Config{Dir: root, Command: host}, "k")
	l, _ := srv.Listen()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, l)
	req, _ := http.NewRequest("POST", "http://"+l.Addr().String()+"/api/run", strings.NewReader(`{"action":"generate-specs","id":"odeme"}`))
	req.Header.Set("X-Sdkit-Key", "k")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %v %v", err, resp)
	}
	for i := 0; i < 100; i++ {
		past, _, c := srv.logFor("odeme").Since(0)
		c()
		for _, e := range past {
			if e.Kind == KindText {
				if e.Text != "T=7200000/14400000" {
					t.Errorf("the host's Bash tool must not end a waiting `sdkit ui ask` after two minutes: %s", e.Text)
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no output")
}

func TestAWithdrawnQuestionIsSaidSo(t *testing.T) {
	root := scratchRepo(t, "")
	srv, _ := panel(t, root, 3)
	cancel := postAsk(t, srv.addr, "odeme")
	cancel() // the host's command ended while the person had not answered
	time.Sleep(400 * time.Millisecond)
	past, _, c := srv.logFor("odeme").Since(0)
	c()
	found := false
	for _, e := range past {
		found = found || e.Name == "ask-expired"
	}
	if !found {
		t.Errorf("the person must be told the question went away: %+v", past)
	}
}
