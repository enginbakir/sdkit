// Package memory is the project memory: durable facts, how-tos and small decisions kept as one file per entry,
// with INDEX.md (one line per entry) as the only file a session loads. Off unless MEMORY=1.
package memory

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usage = `usage:
  sdkit memory status
  sdkit memory init
  sdkit memory add <fact|howto|decision> <slug> --title T [--body TEXT | --body -] [--verify-by YYYY-MM-DD] [--refs a,b]
  sdkit memory index
  sdkit memory search <term>
  sdkit memory stale
  sdkit memory autoload
  sdkit memory candidates <task-slug>
  sdkit memory promote <task-slug> <n...|all>`

var (
	secretRe = regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|token)\s*[=:]\s*\S{6,}|-----BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}`)
	slugRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// Memory works on one project. Get reads a setting (file, then environment).
type Memory struct {
	Root, KitDir string
	Get          func(string) string
	In           io.Reader
	Out, Err     io.Writer
	Now          time.Time
}

func (m *Memory) dir() string {
	d := m.Get("MEMORY_DIR")
	if d == "" {
		d = "memory"
	}
	if filepath.IsAbs(d) || strings.HasPrefix(d, "/") {
		return d
	}
	return filepath.Join(m.Root, m.KitDir, d)
}

func (m *Memory) posInt(key string, def int) int {
	if n, err := strconv.Atoi(m.Get(key)); err == nil && n > 0 {
		return n
	}
	return def
}

// frontmatter returns key's value from the first --- block of an entry.
func frontmatter(text, key string) string {
	in := false
	for _, l := range strings.Split(text, "\n") {
		if l == "---" {
			if in {
				return ""
			}
			in = true
			continue
		}
		if in && strings.HasPrefix(l, key+":") {
			return strings.TrimPrefix(strings.TrimPrefix(l, key+":"), " ")
		}
	}
	return ""
}

// entries lists the entry files (not INDEX.md), sorted by name.
func (m *Memory) entries() []string {
	fs, _ := filepath.Glob(filepath.Join(m.dir(), "*.md"))
	var out []string
	for _, f := range fs {
		if filepath.Base(f) != "INDEX.md" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func (m *Memory) writeIndex() error {
	var b strings.Builder
	b.WriteString("# Knowledge index\n<!-- managed by sdkit memory: one line per entry; read an entry only when its line looks relevant -->\n")
	for _, f := range m.entries() {
		t, _ := os.ReadFile(f)
		s := string(t)
		fmt.Fprintf(&b, "- [%s] %s: %s (updated %s)\n", frontmatter(s, "type"), strings.TrimSuffix(filepath.Base(f), ".md"), frontmatter(s, "title"), frontmatter(s, "updated"))
	}
	b.WriteString("## Linked\n")
	if _, err := os.Stat(filepath.Join(m.Root, m.KitDir, "known-pitfalls.md")); err == nil {
		b.WriteString("- " + m.KitDir + "/known-pitfalls.md: past bug classes\n")
	}
	if st, err := os.Stat(filepath.Join(m.Root, m.KitDir, "adr")); err == nil && st.IsDir() {
		b.WriteString("- " + m.KitDir + "/adr/: architecture decisions\n")
	}
	if err := os.MkdirAll(m.dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dir(), "INDEX.md"), []byte(b.String()), 0o644)
}

// Add writes one entry and rebuilds the index; it returns the exit code.
func (m *Memory) Add(typ, slug, title, body, verifyBy, refs string) int {
	fail := func(code int, msg string) int { fmt.Fprintln(m.Err, msg); return code }
	switch typ {
	case "fact", "howto", "decision":
	default:
		return fail(2, "type must be fact, howto or decision")
	}
	if !slugRe.MatchString(slug) || slug == "INDEX" {
		return fail(2, "slug may contain letters, digits, _ . - only (and not INDEX)")
	}
	if title == "" {
		return fail(2, "--title is required")
	}
	if secretRe.MatchString(title + " " + body) {
		return fail(2, "refused: the text looks like a secret; entries never hold secrets")
	}
	file := filepath.Join(m.dir(), slug+".md")
	if _, err := os.Stat(file); err == nil {
		return fail(1, fmt.Sprintf("entry '%s' exists; edit the file or pick another slug", slug))
	}
	if max := m.posInt("MEMORY_INDEX_MAX", 100); len(m.entries()) >= max {
		return fail(1, fmt.Sprintf("index is full (%d entries, MEMORY_INDEX_MAX); remove or merge a stale entry first (sdkit memory stale)", max))
	}
	text := fmt.Sprintf("---\ntype: %s\ntitle: %s\nupdated: %s\nverify-by: %s\nrefs: %s\n---\n%s\n", typ, title, m.Now.Format("2006-01-02"), verifyBy, refs, body)
	if err := os.MkdirAll(m.dir(), 0o755); err != nil {
		return fail(1, "sdkit: "+err.Error())
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return fail(1, "sdkit: "+err.Error())
	}
	if err := m.writeIndex(); err != nil {
		return fail(1, "sdkit: "+err.Error())
	}
	fmt.Fprintln(m.Out, file)
	return 0
}

// Autoload is INDEX.md trimmed to MEMORY_INDEX_MAX lines, without the trailing newline; empty when off.
func (m *Memory) Autoload() string {
	if m.Get("MEMORY") != "1" || m.Get("MEMORY_AUTOLOAD") == "0" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(m.dir(), "INDEX.md"))
	if err != nil {
		return ""
	}
	ls := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if max := m.posInt("MEMORY_INDEX_MAX", 100); len(ls) > max {
		ls = ls[:max]
	}
	return strings.Join(ls, "\n")
}

// Run executes `sdkit memory <args>` and returns the exit code.
func (m *Memory) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(m.Err, usage)
		return 2
	}
	cmd, a := args[0], args[1:]
	switch cmd {
	case "status", "init", "add", "index", "search", "stale", "autoload", "candidates", "promote":
	default:
		fmt.Fprintln(m.Err, usage)
		return 2
	}
	if m.Get("MEMORY") != "1" {
		if cmd == "status" {
			fmt.Fprintln(m.Out, "memory: off (set MEMORY=1 in sdkit.env)")
		} else if cmd != "autoload" {
			fmt.Fprintln(m.Err, "memory is off (set MEMORY=1 in sdkit.env)")
		}
		return 0
	}
	dir, index := m.dir(), filepath.Join(m.dir(), "INDEX.md")
	bad := func(err error) int { fmt.Fprintln(m.Err, "sdkit:", err); return 1 }
	switch cmd {
	case "status":
		fmt.Fprintf(m.Out, "memory: on, dir %s, %d entries (max %d)\n", dir, len(m.entries()), m.posInt("MEMORY_INDEX_MAX", 100))
	case "init":
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return bad(err)
		}
		if _, err := os.Stat(index); err != nil {
			if err := m.writeIndex(); err != nil {
				return bad(err)
			}
		}
		fmt.Fprintln(m.Out, index)
	case "index":
		if err := m.writeIndex(); err != nil {
			return bad(err)
		}
		fmt.Fprintln(m.Out, index)
	case "add":
		if len(a) < 2 {
			fmt.Fprintln(m.Err, usage)
			return 2
		}
		var title, body, vby, refs string
		for i := 2; i < len(a); i += 2 {
			v := ""
			if i+1 < len(a) {
				v = a[i+1]
			}
			switch a[i] {
			case "--title":
				title = v
			case "--body":
				body = v
			case "--verify-by":
				vby = v
			case "--refs":
				refs = v
			default:
				fmt.Fprintln(m.Err, "unknown option "+a[i])
				return 2
			}
		}
		if body == "-" {
			b, _ := io.ReadAll(m.In)
			body = strings.TrimRight(string(b), "\n")
		}
		return m.Add(a[0], a[1], title, body, vby, refs)
	case "search":
		if len(a) == 0 || a[0] == "" {
			fmt.Fprintln(m.Err, usage)
			return 2
		}
		term, n := strings.ToLower(a[0]), 0
		for _, f := range m.entries() {
			t, _ := os.ReadFile(f)
			for i, l := range strings.Split(strings.TrimRight(string(t), "\n"), "\n") {
				if !strings.Contains(strings.ToLower(l), term) || l == "---" || strings.HasPrefix(l, "updated:") || strings.HasPrefix(l, "verify-by:") || strings.HasPrefix(l, "refs:") {
					continue
				}
				if n++; n > 20 {
					return 0
				}
				out := fmt.Sprintf("%s:%d:%s", filepath.Base(f), i+1, l)
				if len(out) > 200 {
					out = out[:200]
				}
				fmt.Fprintln(m.Out, out)
			}
		}
	case "stale":
		days := m.posInt("MEMORY_STALE_DAYS", 90)
		cutoff, today := m.Now.AddDate(0, 0, -days).Format("2006-01-02"), m.Now.Format("2006-01-02")
		for _, f := range m.entries() {
			t, _ := os.ReadFile(f)
			s, why := string(t), ""
			if u := frontmatter(s, "updated"); u != "" && u < cutoff {
				why += fmt.Sprintf(" older than %d days;", days)
			}
			if v := frontmatter(s, "verify-by"); v != "" && v <= today {
				why += " verify-by " + v + " passed;"
			}
			for _, r := range strings.FieldsFunc(frontmatter(s, "refs"), func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
				if _, err := os.Stat(filepath.Join(m.Root, r)); err != nil {
					why += " ref " + r + " is gone;"
				}
			}
			if why != "" {
				fmt.Fprintf(m.Out, "%s:%s\n", strings.TrimSuffix(filepath.Base(f), ".md"), why)
			}
		}
	case "autoload":
		if s := m.Autoload(); s != "" {
			fmt.Fprintln(m.Out, s)
		}
	default: // candidates, promote
		if len(a) == 0 || a[0] == "" {
			fmt.Fprintln(m.Err, usage)
			return 2
		}
		task, picks := a[0], a[1:]
		if m.Get("MEMORY_PROMOTE") == "off" {
			fmt.Fprintln(m.Err, "promotion is off (MEMORY_PROMOTE=off)")
			return 0
		}
		f, err := os.Open(filepath.Join(m.Root, m.KitDir, "work", task, "notes.md"))
		if err != nil {
			fmt.Fprintf(m.Err, "no notes at work/%s/notes.md\n", task)
			return 0
		}
		defer f.Close()
		var cands []string
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if l, ok := strings.CutPrefix(sc.Text(), "MEMORY:"); ok {
				cands = append(cands, strings.TrimSpace(l))
			}
		}
		if cmd == "candidates" {
			for i, c := range cands {
				fmt.Fprintf(m.Out, "%d: %s\n", i+1, c)
			}
			return 0
		}
		if len(picks) == 0 {
			fmt.Fprintln(m.Err, usage)
			return 2
		}
		rc := 0
		for i, c := range cands {
			chosen := false
			for _, p := range picks {
				chosen = chosen || p == "all" || p == strconv.Itoa(i+1)
			}
			if !chosen {
				continue
			}
			p := append(strings.SplitN(c, "|", 4), "", "", "")
			if m.Add(strings.TrimSpace(p[0]), strings.TrimSpace(p[1]), strings.TrimSpace(p[2]), strings.TrimSpace(p[3]), "", "") != 0 {
				rc = 1
			}
		}
		return rc
	}
	return 0
}
