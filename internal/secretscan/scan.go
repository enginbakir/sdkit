package secretscan

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
)

// Options select what is scanned. With neither History nor Staged the working tree is scanned.
type Options struct {
	Root    string
	History bool   // every line added in any ref
	Staged  bool   // the staged change
	Since   string // History only: commits not reachable from this ref
}

const (
	maxFile  = 2 << 20 // a tracked file larger than this is skipped (generated or vendored data)
	maxLine  = 64 << 10
	histWait = 15 * time.Minute
)

// Scan returns the findings of one source, deduplicated by fingerprint, in the order they were found.
func Scan(o Options) ([]Finding, error) {
	if o.Since != "" && !o.History {
		return nil, fmt.Errorf("--since needs --history")
	}
	switch {
	case o.History && o.Staged:
		return nil, fmt.Errorf("--history and --staged are separate scans")
	case o.History:
		args := []string{"log", "--all", "-p", "--no-color", "-U0", "--format=commit %H"}
		if o.Since != "" {
			sha := gitx.RevParse(o.Root, o.Since)
			if sha == "" {
				return nil, fmt.Errorf("--since %q is not a commit", o.Since)
			}
			args = append(args, "^"+sha)
		}
		return scanDiff(o.Root, args, SourceHistory)
	case o.Staged:
		return scanDiff(o.Root, []string{"diff", "--cached", "--no-color", "-U0"}, SourceStaged)
	}
	return scanTree(o.Root)
}

// collector adds findings once per source and fingerprint.
type collector struct {
	source string
	seen   map[string]bool
	out    []Finding
}

func (c *collector) line(path string, n int, commit, text string) {
	lower := strings.ToLower(text)
	for _, r := range Rules {
		if !r.Match(text, lower) {
			continue
		}
		for _, m := range r.Re.FindAllString(text, -1) {
			fp := Fingerprint(r.ID, path, m)
			if c.seen[fp] {
				continue
			}
			c.seen[fp] = true
			c.out = append(c.out, Finding{Rule: r.ID, Severity: r.Severity, Source: c.source, Path: path, Line: n, Commit: commit, Excerpt: excerpt(text), Fingerprint: fp})
		}
	}
}

// scanTree reads tracked files and untracked files that are not ignored.
func scanTree(root string) ([]Finding, error) {
	tracked, err := gitx.Run(root, "ls-files")
	if err != nil {
		return nil, err
	}
	others, _ := gitx.Run(root, "ls-files", "--others", "--exclude-standard")
	c := &collector{source: SourceTree, seen: map[string]bool{}}
	for _, f := range append(gitx.Lines(tracked), gitx.Lines(others)...) {
		if filepath.Base(f) == BaselineFile {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(f))
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() || st.Size() > maxFile {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		for i, l := range strings.Split(string(b), "\n") {
			c.line(f, i+1, "", strings.TrimRight(l, "\r"))
		}
	}
	return c.out, nil
}

// scanDiff streams `git log -p` or `git diff` output: history can be gigabytes, so it is never held in memory.
func scanDiff(root string, args []string, source string) ([]Finding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), histWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "-c", "core.quotePath=false"}, args...)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &collector{source: source, seen: map[string]bool{}}
	parseDiff(out, c)
	if err := cmd.Wait(); err != nil {
		return c.out, fmt.Errorf("git %s: %w", args[0], err)
	}
	return c.out, nil
}

// parseDiff walks a unified diff and hands every added line to c with its path, new-file line number and commit.
func parseDiff(r io.Reader, c *collector) {
	br := bufio.NewReaderSize(r, 128<<10)
	commit, path, n := "", "", 0
	for {
		l, ok := readLine(br)
		if !ok {
			return
		}
		switch {
		case strings.HasPrefix(l, "commit "):
			commit, path = l[7:min(len(l), 7+12)], ""
		case strings.HasPrefix(l, "+++ b/"):
			path = l[6:]
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			if strings.HasPrefix(l, "+++ /dev/null") {
				path = ""
			}
		case strings.HasPrefix(l, "@@"):
			n = hunkStart(l)
		case strings.HasPrefix(l, "+"):
			if path != "" && filepath.Base(path) != BaselineFile {
				c.line(path, n, commit, strings.TrimRight(l[1:], "\r"))
			}
			n++
		}
	}
}

// hunkStart reads the new-file start line out of "@@ -a,b +c,d @@".
func hunkStart(h string) int {
	i := strings.Index(h, "+")
	if i < 0 {
		return 0
	}
	f := h[i+1:]
	if j := strings.IndexAny(f, ", "); j >= 0 {
		f = f[:j]
	}
	n, _ := strconv.Atoi(f)
	return n
}

// readLine returns one line without its newline; a line longer than maxLine is cut, the rest discarded.
func readLine(br *bufio.Reader) (string, bool) {
	var b []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(b) < maxLine {
			b = append(b, chunk...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && len(chunk) == 0 && len(b) == 0 {
			return "", false
		}
		return strings.TrimRight(string(b), "\r\n"), true
	}
}
