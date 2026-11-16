package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A capability's spec.md is cut into items: preamble (P), `## ` headings (H)
// and `### Requirement:` blocks (R); only R items are ever edited.
type item struct {
	kind, sec string
	lines     []string
}

var (
	h2Re      = regexp.MustCompile(`^## +`)
	reqHeadRe = regexp.MustCompile(`^### +Requirement:`)
	scenIDRe  = longest(`^####[[:space:]]+Scenario:[[:space:]]*(AC-[A-Za-z0-9._-]*-[0-9]+)`)
)

func splitItems(text string) []*item {
	items := []*item{{kind: "P"}}
	sec := "NONE"
	for _, l := range fileLines(text) {
		cur := items[len(items)-1]
		switch {
		case strings.HasPrefix(l, "## "):
			t := h2Re.ReplaceAllString(l, "")
			sec = "NONE"
			if sectionR.MatchString(t) {
				sec = strings.SplitN(t, " ", 2)[0]
			}
			items = append(items, &item{kind: "H", lines: []string{l}})
		case reqHeadRe.MatchString(l):
			items = append(items, &item{kind: "R", sec: sec, lines: []string{l}})
		default:
			cur.lines = append(cur.lines, l)
		}
	}
	if text == "" {
		items[0].lines = nil
	}
	return items
}

func idsOf(lines []string) []string {
	var ids []string
	for _, l := range lines {
		if m := scenIDRe.FindStringSubmatch(l); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func titleOf(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	t := regexp.MustCompile(`^###[[:space:]]+Requirement:[[:space:]]*`).ReplaceAllString(lines[0], "")
	if t == lines[0] {
		return ""
	}
	return strings.TrimRight(t, " \t\r\v\f")
}

func isWithdrawn(lines []string, id string) bool {
	re := regexp.MustCompile(`^####[[:space:]]+Scenario:[[:space:]]*` + regexp.QuoteMeta(id) + `[[:space:]]*\([Ww]ithdrawn\)`)
	for _, l := range lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// headLvl is the number of leading # of a `#+[ \t]` line, else 0.
func headLvl(l string) int {
	n := 0
	for n < len(l) && l[n] == '#' {
		n++
	}
	if n > 0 && n < len(l) && (l[n] == ' ' || l[n] == '\t') {
		return n
	}
	return 0
}

// scenarioOf reports whether l is the level-4 scenario heading of id, and where the id ends.
func scenarioOf(l, id string, lvl int) (int, bool) {
	if lvl != 4 || !strings.Contains(l, "Scenario:") {
		return 0, false
	}
	loc := idRe.FindStringIndex(l)
	if loc == nil {
		return 0, false
	}
	return loc[1], l[loc[0]:loc[1]] == id
}

func acBlock(lines []string, id string) []string {
	var out []string
	on := false
	for _, l := range lines {
		if lvl := headLvl(l); lvl > 0 {
			if on && lvl <= 4 {
				on = false
			}
			if _, ok := scenarioOf(l, id, lvl); !on && ok {
				on = true
			}
		}
		if on {
			out = append(out, l)
		}
	}
	return out
}

var (
	wdHeadRe = regexp.MustCompile(`^[ \t]*\([Ww]ithdrawn\)`)
	dashRe   = regexp.MustCompile(`^[ \t]*-?[ \t]*`)
)

func withdrawAC(lines []string, id, slug string) []string {
	var out []string
	skip := false
	for _, l := range lines {
		if lvl := headLvl(l); lvl > 0 {
			if skip && lvl <= 4 {
				skip = false
			}
			if end, ok := scenarioOf(l, id, lvl); !skip && ok {
				t := dashRe.ReplaceAllString(wdHeadRe.ReplaceAllString(l[end:], ""), "")
				out = append(out, fmt.Sprintf("#### Scenario: %s (withdrawn) - %s", id, t),
					fmt.Sprintf("- **THEN** WITHDRAWN: removed by change %s. The id is kept and never reused", slug), "")
				skip = true
				continue
			}
		}
		if !skip {
			out = append(out, l)
		}
	}
	return out
}

func lastBlank(lines []string) bool {
	return len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == ""
}

func joinItems(items []*item) string {
	var b strings.Builder
	for _, it := range items {
		for _, l := range it.lines {
			b.WriteString(l + "\n")
		}
		if !lastBlank(it.lines) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Delta writes the change's delta into the base files of root: ADDED requirements
// are appended, a MODIFIED requirement replaces the base requirement it shares
// an AC id with (withdrawn base ACs it omits are kept), a REMOVED AC (or a bare
// requirement, matched by title) is rewritten as `(withdrawn)`; the id stays.
func Delta(root, slug string) error {
	c := filepath.Join(root, "changes", slug)
	if st, err := os.Stat(filepath.Join(c, "specs")); err != nil || !st.IsDir() {
		return fmt.Errorf("no delta in %s", c)
	}
	dfs, _ := filepath.Glob(filepath.Join(c, "specs", "*", "spec.md"))
	sort.Strings(dfs)
	for _, df := range dfs {
		capab := filepath.Base(filepath.Dir(df))
		bf := filepath.Join(root, "specs", capab, "spec.md")
		if _, err := os.Stat(bf); err != nil {
			if err := os.MkdirAll(filepath.Dir(bf), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(bf, []byte(fmt.Sprintf("# %s\n\n## Requirements\n\n", capab)), 0o644); err != nil {
				return err
			}
		}
		btxt, err1 := os.ReadFile(bf)
		dtxt, err2 := os.ReadFile(df)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("cannot read %s", capab)
		}
		base := splitItems(string(btxt))
		rItems := func() (r []*item) {
			for _, it := range base {
				if it.kind == "R" {
					r = append(r, it)
				}
			}
			return
		}
		for _, di := range splitItems(string(dtxt)) {
			if di.kind != "R" {
				continue
			}
			dids := idsOf(di.lines)
			switch di.sec {
			case "ADDED":
				base = append(base, &item{kind: "R", sec: "ADDED", lines: di.lines})
			case "MODIFIED":
				var hit *item
				for _, bi := range rItems() {
					for _, i := range dids {
						if has(idsOf(bi.lines), i) {
							hit = bi
							break
						}
					}
					if hit != nil {
						break
					}
				}
				if hit == nil {
					return fmt.Errorf("%s: MODIFIED requirement '%s' matches nothing in %s", slug, titleOf(di.lines), capab)
				}
				old := hit.lines
				hit.lines = append([]string{}, di.lines...)
				if !lastBlank(hit.lines) {
					hit.lines = append(hit.lines, "")
				}
				for _, i := range idsOf(old) {
					if isWithdrawn(old, i) && !has(dids, i) {
						hit.lines = append(hit.lines, acBlock(old, i)...)
					}
				}
			case "REMOVED":
				if len(dids) > 0 {
					for _, i := range dids {
						for _, bi := range rItems() {
							if has(idsOf(bi.lines), i) {
								if !isWithdrawn(bi.lines, i) {
									bi.lines = withdrawAC(bi.lines, i, slug)
								}
								break
							}
						}
					}
					continue
				}
				t, found := titleOf(di.lines), false
				for _, bi := range rItems() {
					if titleOf(bi.lines) != t {
						continue
					}
					found = true
					for _, i := range idsOf(bi.lines) {
						if !isWithdrawn(bi.lines, i) {
							bi.lines = withdrawAC(bi.lines, i, slug)
						}
					}
				}
				if !found {
					return fmt.Errorf("%s: REMOVED requirement '%s' matches nothing in %s", slug, t, capab)
				}
			}
		}
		if err := os.WriteFile(bf, []byte(joinItems(base)), 0o644); err != nil {
			return fmt.Errorf("cannot write %s", bf)
		}
	}
	return nil
}

// recordRename adds old->new to renames and rewrites old in touches, keeping the key order of change.json.
func recordRename(cj, old, nw string) error {
	ps, err := readObj(cj)
	if err != nil {
		return err
	}
	tr := field(ps, "touches")
	var touches []string
	if tr == nil || json.Unmarshal(tr, &touches) != nil || string(tr) == "null" {
		return fmt.Errorf("no touches")
	}
	for i, t := range touches {
		if t == old {
			touches[i] = nw
		}
	}
	rn, _ := okv(field(ps, "renames"))
	set := func(ps []pair, k string, v json.RawMessage) []pair {
		for i := range ps {
			if ps[i].K == k {
				ps[i].V = v
				return ps
			}
		}
		return append(ps, pair{k, v})
	}
	nv, _ := json.Marshal(nw)
	rn = set(rn, old, nv)
	rb := []byte("{")
	for i, p := range rn {
		k, _ := json.Marshal(p.K)
		if i > 0 {
			rb = append(rb, ',')
		}
		rb = append(append(append(rb, k...), ':'), p.V...)
	}
	rb = append(rb, '}')
	tb, _ := json.Marshal(touches)
	ps = set(set(ps, "renames", rb), "touches", tb)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range ps {
		k, _ := json.Marshal(p.K)
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(append(append(k, ':'), p.V...))
	}
	b.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, b.Bytes(), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	return os.WriteFile(cj, out.Bytes(), 0o644)
}

// Renumber renumbers the AC ids the changes ADD that the base or an earlier
// change (createdAt order) already holds, to the next free number of their
// prefix, in the later change's own *.md files, and records them in its
// change.json `renames`. It prints `<slug> <old> <new>` per rename.
func Renumber(root string, slugs []string, out io.Writer) error {
	keyed := []string{}
	for _, s := range slugs {
		ca := ""
		if ps, err := readObj(filepath.Join(root, "changes", s, "change.json")); err == nil {
			ca = strOf(field(ps, "createdAt"))
		}
		keyed = append(keyed, ca+"\t"+s)
	}
	sort.Strings(keyed)
	b, err := Parse(root)
	if err != nil {
		return err
	}
	var baseids, claimed []string
	for _, a := range b.ACs {
		baseids = append(baseids, a.ID)
	}
	claimed = append(claimed, baseids...)
	for _, k := range keyed {
		s := k[strings.Index(k, "\t")+1:]
		c := filepath.Join(root, "changes", s)
		d, err := Parse(c)
		if err != nil {
			return err
		}
		var mine, own []string
		secOf := map[string]string{}
		for _, a := range d.ACs {
			own = append(own, a.ID)
			if _, ok := secOf[a.ID]; !ok {
				secOf[a.ID] = a.Section
			}
		}
		for _, a := range d.ACs {
			id := a.ID
			if a.Section == "REMOVED" {
				continue
			}
			if !has(claimed, id) || !(secOf[id] == "ADDED" || !has(baseids, id)) {
				mine = append(mine, id)
				continue
			}
			pre, max := id[:strings.LastIndex(id, "-")], 0
			for _, x := range append(append([]string{}, claimed...), own...) {
				if strings.HasPrefix(x, pre+"-") {
					if n, err := strconv.Atoi(x[len(pre)+1:]); err == nil && n > max {
						max = n
					}
				}
			}
			nw := fmt.Sprintf("%s-%d", pre, max+1)
			re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(id) + `([^0-9A-Za-z_]|$)`)
			_ = filepath.WalkDir(c, func(p string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() || !strings.HasSuffix(p, ".md") {
					return nil
				}
				raw, _ := os.ReadFile(p)
				ls := strings.Split(string(raw), "\n")
				for i, l := range ls {
					ls[i] = re.ReplaceAllString(re.ReplaceAllString(l, "${1}"+nw+"${2}"), "${1}"+nw+"${2}")
				}
				if txt := strings.Join(ls, "\n"); txt != string(raw) {
					_ = os.WriteFile(p, []byte(txt), 0o644)
				}
				return nil
			})
			if err := recordRename(filepath.Join(c, "change.json"), id, nw); err != nil {
				return fmt.Errorf("cannot record the rename in %s/change.json", c)
			}
			claimed = append(claimed, nw)
			fmt.Fprintf(out, "%s %s %s\n", s, id, nw)
		}
		for _, id := range mine {
			if !has(claimed, id) {
				claimed = append(claimed, id)
			}
		}
	}
	return nil
}

const applyUsage = "usage: sdkit spec apply renumber <spec-root> <slug>... | delta <spec-root> <slug>"

// RunApply runs `sdkit spec apply <verb> ...`, the file-writing half. Exit 0 ok, 1 a step failed, 2 usage.
func RunApply(args []string, out, errw io.Writer) int {
	isDir := func(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
	var err error
	switch {
	case len(args) >= 3 && args[0] == "renumber" && isDir(args[1]):
		err = Renumber(args[1], args[2:], out)
	case len(args) >= 3 && args[0] == "delta" && isDir(args[1]) && args[2] != "":
		err = Delta(args[1], args[2])
	default:
		fmt.Fprintln(errw, applyUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(errw, "spec-apply: %v\n", err)
		return 1
	}
	return 0
}

// Run dispatches `sdkit spec change|apply ...`.
func Run(args []string, out, errw io.Writer) int {
	if len(args) > 0 && args[0] == "change" {
		return RunChange(args[1:], out, errw)
	}
	if len(args) > 0 && args[0] == "apply" {
		return RunApply(args[1:], out, errw)
	}
	fmt.Fprintln(errw, changeUsage+"\n"+applyUsage)
	return 2
}
