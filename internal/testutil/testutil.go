// Package testutil holds the helpers shared by the Go tests: a scratch git
// repo, the built sdkit binary, golden files and fake CLIs on PATH.
package testutil

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func root(t testing.TB) string {
	t.Helper()
	d, _ := os.Getwd()
	for ; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			t.Fatal("go.mod not found")
		}
	}
}

// cleanEnv drops the variables a host session sets, so tests do not depend on
// the shell they run from (an agent session exports CLAUDE_PROJECT_DIR).
func cleanEnv() (env []string) {
	for _, kv := range os.Environ() {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "CLAUDE_PROJECT_DIR", "CLAUDE_PLUGIN_ROOT", "CURSOR_PROJECT_DIR", "GEMINI_PROJECT_DIR", "SDKIT_HOST", "KIT_DIR", "SDD_MODE", "SDD_SKIP", "VERIFY_BASE", "SDKIT_CI_BASE":
			continue
		}
		env = append(env, kv)
	}
	return env
}

func run(t testing.TB, dir string, env []string, stdin string, name string, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir, c.Env, c.Stdin = dir, append(cleanEnv(), env...), strings.NewReader(stdin)
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out.String(), 0
}

// TempRepo returns a fresh git repository in a temp directory.
func TempRepo(t testing.TB) string {
	t.Helper()
	d := t.TempDir()
	cfg := []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}
	if out, code := run(t, d, cfg, "", "git", "init", "-q", "-b", "main"); code != 0 {
		t.Fatalf("git init: %s", out)
	}
	return d
}

// RunKit runs the sdkit binary (built once per test run) in repo and returns
// its stdout with the repo path replaced by <TMP>, and the exit code.
func RunKit(t testing.TB, repo, stdin string, args ...string) (string, int) {
	t.Helper()
	buildOnce.Do(func() {
		// One dir per test process: packages run in parallel and must not
		// overwrite each other's binary.
		dir, err := os.MkdirTemp("", "sdkit-testbin-")
		if err != nil {
			buildErr = &exec.ExitError{Stderr: []byte(err.Error())}
			return
		}
		binPath = filepath.Join(dir, "sdkit")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}
		out, err := exec.Command("go", "build", "-o", binPath, filepath.Join(root(t), "cmd", "sdkit")).CombinedOutput()
		if err != nil {
			buildErr = &exec.ExitError{Stderr: out}
		}
	})
	if buildErr != nil {
		t.Fatalf("build sdkit: %s", buildErr.(*exec.ExitError).Stderr)
	}
	out, code := run(t, repo, nil, stdin, binPath, args...)
	return strings.ReplaceAll(out, filepath.ToSlash(repo), "<TMP>"), code
}

var ts = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z?`)

// Golden compares got with testdata/golden/<name>.txt (lines starting "# " in
// the file are capture headers and ignored). Run with -update to rewrite it.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	got = ts.ReplaceAllString(strings.ReplaceAll(got, "\r\n", "\n"), "<TS>")
	p := filepath.Join(root(t), "testdata", "golden", filepath.FromSlash(name)+".txt")
	if *update {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, l := range strings.SplitAfter(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(l, "# ") {
			want = append(want, l)
		}
	}
	if w := strings.Join(want, ""); w != got {
		t.Errorf("golden %s differs\nwant:\n%s\ngot:\n%s", name, w, got)
	}
}

// FakeCLI puts an executable called name (running script, a shell body) first
// on PATH for the test.
func FakeCLI(t testing.TB, name, script string) {
	t.Helper()
	d := t.TempDir()
	body, file := "#!/bin/sh\n"+script+"\n", name
	if runtime.GOOS == "windows" {
		body, file = "@echo off\r\n"+script+"\r\n", name+".cmd"
	}
	if err := os.WriteFile(filepath.Join(d, file), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
}
