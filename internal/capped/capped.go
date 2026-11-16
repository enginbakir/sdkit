// Package capped runs a noisy command and keeps what matters in the conversation:
// the exit code, the failure lines and the tail. The full output goes to a log file.
package capped

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

// Options of Run. Args of length 1 is a command string for the shell, longer is argv.
type Options struct {
	Root    string // repository root; the command runs here
	KitDir  string // project directory relative to Root
	Slug    string // "" finds the active task, else _misc
	Lines   int    // <= 0 means 40
	Args    []string
	Timeout time.Duration // 0 means none
}

var failRe = regexp.MustCompile(`(?i)FAIL|ERROR|panic|exception|traceback|not ok|✗|✘|assert`)

// Run executes the command and returns the text to print and the command's exit code.
func Run(ctx context.Context, o Options) (string, int, error) {
	if o.Lines <= 0 {
		o.Lines = 40
	}
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	slug := o.Slug
	if slug == "" {
		slug = ActiveSlug(o.Root, o.KitDir)
	}
	if slug == "" || strings.Trim(slug, "A-Za-z0-9_.-") != "" {
		slug = "_misc"
	}
	dir := filepath.Join(o.Root, o.KitDir, "work", slug, "out")
	if os.MkdirAll(dir, 0o755) != nil {
		dir = os.TempDir()
	}
	logf, err := os.CreateTemp(dir, fmt.Sprintf("cmd-%d-%d-*.log", time.Now().Unix(), os.Getpid()))
	if err != nil {
		return "", 1, err
	}
	logName := logf.Name()
	var cmd *exec.Cmd
	if len(o.Args) == 1 {
		cmd = proc.Shell(ctx, o.Args[0])
	} else {
		cmd = exec.CommandContext(ctx, o.Args[0], o.Args[1:]...)
	}
	cmd.Dir, cmd.Stdout, cmd.Stderr = o.Root, logf, logf
	rc := 0
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() >= 0 {
			rc = ee.ExitCode()
		} else {
			rc = 127
			fmt.Fprintln(logf, "sdkit:", err)
		}
	}
	logf.Close()
	data, _ := os.ReadFile(logName)
	return summarize(data, rc, o.Lines, relTo(o.Root, logName)), rc, nil
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

func cut(s string) string {
	if r := []rune(s); len(r) > 240 {
		return string(r[:240])
	}
	return s
}

func summarize(data []byte, rc, lines int, log string) string {
	text := string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	ls := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		ls = nil
	}
	if len(ls) <= lines {
		return text
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[capped] exit %d, %d lines. Full output: %s\n", rc, len(ls), log)
	n := 0
	var fails []string
	for i, l := range ls {
		if n < 30 && failRe.MatchString(l) {
			fails = append(fails, fmt.Sprintf("%d:%s", i+1, cut(l)))
			n++
		}
	}
	if len(fails) > 0 {
		b.WriteString("--- failure lines (line: text)\n" + strings.Join(fails, "\n") + "\n")
	}
	tail := lines / 2
	fmt.Fprintf(&b, "--- last %d lines\n", tail)
	for _, l := range ls[len(ls)-tail:] {
		b.WriteString(cut(l) + "\n")
	}
	return b.String()
}

// ActiveSlug is the task whose budget and ledger count (see state.ActiveSlug).
func ActiveSlug(root, kitDir string) string {
	return state.ActiveSlug(project.Project{Root: root, KitDir: kitDir})
}
