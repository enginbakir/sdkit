// Package devscan is the publishability scan (sdkit dev scan): is this repository safe to hand out?
// Hard findings (secret-shaped strings, credentials in URLs, quoted credential assignments) fail the scan;
// soft findings (machine paths, names, identities) are listed only. Patterns listed in packaging/scan-allow.txt are ignored.
package devscan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/redact"
)

var (
	secret  = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-ant-[A-Za-z0-9_-]{10,}|sk-[A-Za-z0-9]{32,}|AIza[0-9A-Za-z_-]{35}|ATBB[A-Za-z0-9_=-]{20,}|glpat-[A-Za-z0-9_-]{20,}|eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.`)
	assign  = regexp.MustCompile(`(password|passwd|secret|api[_-]?key|apikey|access[_-]?token|auth[_-]?token|bearer)[A-Za-z_]*[[:space:]]*[:=][[:space:]]*["'][^"'$ ]{8,}["']`)
	urlCred = regexp.MustCompile(`://[^/[:space:]:@]+:[^/[:space:]@]+@[A-Za-z0-9.-]+`)
	soft    = regexp.MustCompile(`/Users/[A-Za-z]|/home/[a-z]+/|C:\\Users`)
	skip    = regexp.MustCompile(`^packaging/(scan-publishable\.sh|scan-allow\.txt)$|^tests/security\.test\.sh$`)
)

type hit struct{ loc, text string }

// Run scans the repository at root, writes the report to w and returns 1 when a hard finding exists, else 0.
func Run(root string, history bool, w io.Writer) int {
	fail := false
	allow := loadAllow(root)
	report := func(label, sev string, hits []hit, mask bool) {
		if len(hits) == 0 {
			return
		}
		fmt.Fprintf(w, "%s [%s]\n", label, sev)
		for i, h := range hits {
			if i == 8 {
				break
			}
			t := h.text
			if mask {
				t = maskLine(t)
			}
			l := h.loc + t
			if len(l) > 160 {
				l = l[:160]
			}
			fmt.Fprintln(w, l)
		}
		if sev == "hard" {
			fail = true
		}
	}
	allowed := func(h hit) bool {
		for _, a := range allow {
			if a.MatchString(h.loc + h.text) {
				return true
			}
		}
		return false
	}
	drop := func(hs []hit) (out []hit) {
		for _, h := range hs {
			if !allowed(h) {
				out = append(out, h)
			}
		}
		return
	}

	tree := scanTree(root)
	find := func(re *regexp.Regexp) (out []hit) {
		for _, h := range tree {
			if re.MatchString(h.text) {
				out = append(out, h)
			}
		}
		return
	}
	report("tree: secret-shaped strings", "hard", find(secret), true)
	report("tree: quoted credential assignments", "hard", find(assign), true)
	report("tree: credentials in URLs", "hard", drop(find(urlCred)), true)
	report("tree: machine paths / names / identities", "soft", drop(find(soft)), false)

	if history {
		added, ids := scanHistory(root)
		var hs, hu []hit
		for _, h := range added {
			if secret.MatchString(h.text) {
				hs = append(hs, h)
			}
			if urlCred.MatchString(h.text) {
				hu = append(hu, h)
			}
		}
		report("history: secret-shaped strings", "hard", drop(hs), true)
		report("history: credentials in URLs", "hard", drop(hu), true)
		var idh []hit
		for _, l := range ids {
			idh = append(idh, hit{text: l})
		}
		report("history: author identities (cannot be removed without rewriting history)", "soft", idh, false)
	}
	if !fail {
		fmt.Fprintln(w, "scan: no hard findings")
		return 0
	}
	fmt.Fprintln(w, "scan: HARD FINDINGS, do not publish")
	return 1
}

func loadAllow(root string) (out []*regexp.Regexp) {
	b, err := os.ReadFile(filepath.Join(root, "packaging", "scan-allow.txt"))
	if err != nil {
		return nil
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || l[0] == '#' {
			continue
		}
		if re, err := regexp.Compile(l); err == nil {
			out = append(out, re)
		}
	}
	return
}

// maskLine keeps the first 4 characters of each hard match and masks the rest, then applies the kit redactor.
func maskLine(l string) string {
	for _, re := range []*regexp.Regexp{secret, assign, urlCred} {
		l = re.ReplaceAllStringFunc(l, func(m string) string {
			if len(m) <= 4 {
				return "***"
			}
			return m[:4] + "***"
		})
	}
	return redact.Line(l)
}

func scanTree(root string) (out []hit) {
	files, err := gitx.Run(root, "ls-files")
	if err != nil {
		return nil
	}
	for _, f := range gitx.Lines(files) {
		if skip.MatchString(f) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		for i, l := range strings.Split(string(b), "\n") {
			out = append(out, hit{fmt.Sprintf("%s:%d:", f, i+1), strings.TrimRight(l, "\r")})
		}
	}
	return
}

// scanHistory returns every line added in any ref (located as commit:path:) and the unique author identities with counts.
func scanHistory(root string) (added []hit, ids []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := proc.Run(ctx, root, nil, "git", "-c", "core.quotePath=false", "log", "--all", "-p", "--no-color", "-U0", "--format=commit %H")
	if err != nil {
		return
	}
	commit, path := "", ""
	for _, l := range strings.Split(res.Stdout, "\n") {
		switch {
		case strings.HasPrefix(l, "commit "):
			commit, path = l[7:min(len(l), 7+12)], ""
		case strings.HasPrefix(l, "+++ b/"):
			path = l[6:]
		case strings.HasPrefix(l, "+++"):
		case strings.HasPrefix(l, "+"):
			if skip.MatchString(path) {
				continue
			}
			added = append(added, hit{commit + ":" + path + ":", l[1:]})
		}
	}
	if out, err := gitx.Run(root, "log", "--all", "--format=%an <%ae>"); err == nil {
		n := map[string]int{}
		for _, l := range gitx.Lines(out) {
			n[l]++
		}
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ids = append(ids, fmt.Sprintf("%7d %s", n[k], k))
		}
	}
	return
}
