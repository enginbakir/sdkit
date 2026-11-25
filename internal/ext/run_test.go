package ext

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The decision under test: a question the agent asks reaches the extension as a line, and the answer line goes back to the agent's
// waiting call; the last line is always "end". The host is a script that asks over the bridge with curl, as `sdkit ui ask` does.
func TestRunRelaysQuestionAndAnswer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh host")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl needed by the stand-in host")
	}
	root := t.TempDir()
	for _, c := range [][]string{{"init", "-q"}, {"-c", "user.email=a@b", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		cmd := exec.Command("git", c...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	os.MkdirAll(filepath.Join(root, ".sdkit"), 0o755)
	os.WriteFile(filepath.Join(root, ".sdkit", "sdkit.env"), []byte("BASE_BRANCH=master\n"), 0o644)
	host := filepath.Join(t.TempDir(), "host")
	os.WriteFile(host, []byte(`#!/bin/sh
echo '{"type":"system","session_id":"abcdef12-0000-0000-0000-000000000001"}'
A=$(curl -s -X POST -H "X-Sdkit-Key: $SDKIT_UI_KEY" -d '{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}' "$SDKIT_UI_URL/api/ask")
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"got %s"}]}}\n' "$(echo "$A" | tr -d '"{}')"
echo '{"type":"result","is_error":false,"result":"ok"}'
`), 0o755)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{Root: root, Action: "generate-specs", ID: "demo", Command: host, Secret: "k", Tools: []string{"Bash"}}, inR, outW)
		outW.Close()
	}()

	var texts []string
	var last Out
	sc := bufio.NewScanner(outR)
	timeout := time.AfterFunc(20*time.Second, func() { inW.Close() })
	defer timeout.Stop()
	for sc.Scan() {
		var o Out
		if err := json.Unmarshal(sc.Bytes(), &o); err != nil {
			t.Fatalf("not a JSON line: %s", sc.Bytes())
		}
		last = o
		switch {
		case o.Type == "question":
			if !bytes.Contains(o.Questions, []byte("Q?")) {
				t.Errorf("question lost its text: %s", o.Questions)
			}
			io.WriteString(inW, `{"type":"answer","id":`+itoa(o.ID)+`,"answers":{"Q?":"A"}}`+"\n")
		case o.Type == "event" && o.Event.Kind == "text":
			texts = append(texts, o.Event.Text)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if last.Type != "end" || last.Failed {
		t.Errorf("last line = %+v, want a clean end", last)
	}
	if len(texts) != 1 || !strings.Contains(texts[0], "Q?:A") {
		t.Errorf("the agent did not get the answer: %q", texts)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
