// Package digest makes bounded views of long text for the orchestrator's context (F09). The full text stays on disk and is
// named in the view; a failure line is never dropped. A view is never a verdict: that comes from the verdict parser on the full answer.
package digest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/redact"
	"github.com/wecii/sdkit/internal/store"
)

// D is where views are noted. Dir is the absolute project directory, Slug the active task ("" is _none).
type D struct {
	Dir, Slug string
	Stats     bool // false: write no context.jsonl (DIGEST_STATS=0)
	MinBytes  int  // answers up to this size pass through the envelope unchanged
	Now       func() time.Time
}

var failRE = regexp.MustCompile(`FAIL|ERROR|[Pp]anic|Traceback|error:|not ok|BLOCK|CONFIRMED|[Ee]xception|CRITICAL|Segmentation`)

var (
	verdictRE = regexp.MustCompile("^[[:space:]]*[*_]*VERDICT[*_]*[[:space:]]*:?[*_[:space:]]*:?[[:space:]]*[*_`]*(PASS|BLOCK|pass|block)[*_`]*[[:space:]]*$")
	passRE    = regexp.MustCompile(`(?i)\|[[:space:]]*[*_]*PASS[*_]*[[:space:]]*(\||$)`)
	failRowRE = regexp.MustCompile(`(?i)\|[[:space:]]*[*_]*FAIL[*_]*[[:space:]]*(\||$)`)
	confRE    = regexp.MustCompile(`(?i)^[[:space:]]*-[[:space:]]*CONFIRMED`)
	plausRE   = regexp.MustCompile(`(?i)^[[:space:]]*-[[:space:]]*PLAUSIBLE`)
	critRE    = regexp.MustCompile(`^[[:space:]]*([-*]|[0-9]+[.)])?[[:space:]]*[*_]*(BLOCKER|CRITICAL|HIGH)[*_]*[[:space:]]*\|`)
	keepRE    = regexp.MustCompile(`\|[[:space:]]*[*_]*FAIL|^[[:space:]]*-[[:space:]]*CONFIRMED|^[[:space:]]*([-*]|[0-9]+[.)])?[[:space:]]*[*_]*(BLOCKER|CRITICAL|HIGH)[*_]*[[:space:]]*\||NEEDS-LOOKUP|HANDOFF-RESULT|^SUMMARY:|^VERDICT|result:[[:space:]]*red`)
	fenceRE   = regexp.MustCompile("^[[:space:]]*```")
)

func (d D) slug() string {
	if d.Slug == "" {
		return "_none"
	}
	return d.Slug
}

func (d D) note(kind string, full, shown int) {
	if !d.Stats {
		return
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	store.AppendLine(filepath.Join(d.Dir, "work", d.slug(), "context.jsonl"),
		fmt.Sprintf(`{"ts":"%s","kind":"%s","full_bytes":%d,"shown_bytes":%d}`, now().UTC().Format("2006-01-02T15:04:05Z"), kind, full, shown))
}

// NewRef is a fresh path for a full text.
func (d D) NewRef(label string) string {
	return filepath.Join(d.Dir, "work", d.slug(), "out", fmt.Sprintf("%s-%d-%d.txt", label, time.Now().Unix(), os.Getpid()))
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func lines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Output is the bounded view of a command's output: at most max lines pass unchanged; longer, the first 10 lines, every failure
// line (up to 80), the last 15 and the path of the full text, which is written to ref (a fresh path when empty).
func (d D) Output(text string, max int, ref, label string) string {
	n := strings.Count(text, "\n")
	if n <= max {
		d.note("output", len(text), len(text))
		return text
	}
	if ref == "" {
		ref = d.NewRef(label)
	}
	os.MkdirAll(filepath.Dir(ref), 0o755)
	os.WriteFile(ref, []byte(text), 0o644)
	ls := lines(text)
	var b []string
	b = append(b, ls[:min(10, len(ls))]...)
	b = append(b, "[... failure lines of the full text (line: text) ...]")
	k := 0
	for i, l := range ls {
		if k < 80 && failRE.MatchString(l) {
			b = append(b, cut(strconv.Itoa(i+1)+":"+l, 300))
			k++
		}
	}
	b = append(b, "[... last 15 lines ...]")
	b = append(b, ls[max0(len(ls)-15):]...)
	v := redact.Text(strings.Join(b, "\n") + "\n")
	d.note("output", len(text), len(v))
	return fmt.Sprintf("%s[digest: %d lines, %d bytes kept; the full text is in %s]\n", v, n, len(text), ref)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func count(ls []string, re *regexp.Regexp) int {
	c := 0
	for _, l := range ls {
		if re.MatchString(l) {
			c++
		}
	}
	return c
}

// Envelope is the compact ENVELOPE/1 view of a role's final answer; a short one is returned unchanged. The full answer is written to
// ref when it does not exist yet. No trailing newline.
func (d D) Envelope(answer, role, ref string) string {
	fb := len(answer)
	if fb <= d.MinBytes {
		d.note("envelope", fb, fb)
		return answer
	}
	if ref == "" {
		ref = d.NewRef("out")
	}
	if _, err := os.Stat(ref); err != nil {
		os.MkdirAll(filepath.Dir(ref), 0o755)
		os.WriteFile(ref, []byte(answer), 0o644)
	}
	var cl []string
	for _, l := range lines(strings.ReplaceAll(answer, "\r", "")) {
		if !fenceRE.MatchString(l) {
			cl = append(cl, l)
		}
	}
	dec := "none"
	for _, l := range cl {
		if m := verdictRE.FindStringSubmatch(l); m != nil {
			dec = strings.ToUpper(m[1])
		}
	}
	var keep []string
	for _, l := range cl {
		if keepRE.MatchString(l) {
			keep = append(keep, cut(l, 400))
		}
	}
	nxt := "read the evidence file only for the lines above"
	switch dec {
	case "BLOCK":
		nxt = "fix the FAIL/CONFIRMED items above, then run the independent role again on the new diff"
	case "PASS":
		nxt = "record via parse-verdict.sh --record (the envelope is not the verdict)"
	}
	body := []string{fmt.Sprintf("ENVELOPE/1 role=%s decision=%s dod_pass=%d dod_fail=%d confirmed=%d plausible=%d blocking_severity=%d evidence=%s",
		role, dec, count(cl, passRE), count(cl, failRowRE), count(cl, confRE), count(cl, plausRE), count(cl, critRE), ref)}
	body = append(body, keep...)
	if dec == "none" {
		for _, l := range cl[:min(12, len(cl))] {
			body = append(body, cut(l, 300))
		}
	}
	body = append(body, "NEXT: "+nxt)
	text := strings.Join(body, "\n") + "\n"
	shown := len(text)
	d.note("envelope", fb, shown)
	return redact.Text(text) + fmt.Sprintf("(bytes full=%d shown=%d; the envelope is a view, not a verdict: the verdict is parse-verdict.sh on the full answer)", fb, shown)
}

// Summary is the bytes kept off the context so far, one line per kind.
func (d D) Summary() string {
	b, err := os.ReadFile(filepath.Join(d.Dir, "work", d.slug(), "context.jsonl"))
	if err != nil {
		return "digest: no context.jsonl for " + d.slug() + "\n"
	}
	type agg struct{ n, full, shown int }
	m := map[string]*agg{}
	for _, l := range lines(string(b)) {
		var k string
		var f, s int
		if i := strings.Index(l, `"kind":"`); i >= 0 {
			k = l[i+8 : i+8+strings.Index(l[i+8:], `"`)]
		}
		f = num(l, `"full_bytes":`)
		s = num(l, `"shown_bytes":`)
		if m[k] == nil {
			m[k] = &agg{}
		}
		m[k].n++
		m[k].full += f
		m[k].shown += s
	}
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var o strings.Builder
	for _, k := range ks {
		a := m[k]
		fmt.Fprintf(&o, "%s: %d view(s), full %d bytes, shown %d bytes, kept off context %d bytes\n", k, a.n, a.full, a.shown, a.full-a.shown)
	}
	return o.String()
}

func num(l, key string) int {
	i := strings.Index(l, key)
	if i < 0 {
		return 0
	}
	j := i + len(key)
	e := j
	for e < len(l) && l[e] >= '0' && l[e] <= '9' {
		e++
	}
	n, _ := strconv.Atoi(l[j:e])
	return n
}
