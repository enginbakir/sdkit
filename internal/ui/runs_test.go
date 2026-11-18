package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
