package plan

import (
	"regexp"
	"sort"
	"strings"
)

var (
	reDecor   = regexp.MustCompile("[*_`|]")
	reBlanks  = regexp.MustCompile(`[ \t]+`)
	reTail    = regexp.MustCompile(`[ .;,:—–-]+$`)
	reTaskRow = regexp.MustCompile(`^[ \t]*- \[[ xX]\] +[A-Za-z0-9_.-]+`)
	reTaskPre = regexp.MustCompile(`^[ \t]*- \[[ xX]\] +`)
)

func norm(s string) string {
	s = reBlanks.ReplaceAllString(reDecor.ReplaceAllString(s, " "), " ")
	return reTail.ReplaceAllString(strings.TrimPrefix(s, " "), "")
}

// dodItems returns "D<n>\ttext\tverified-by" per item, lowercased and normalised, sorted.
func dodItems(text string) []string {
	var out []string
	for _, l := range strings.Split(section(text, "definition of done"), "\n") {
		m := reItem.FindStringSubmatchIndex(l)
		if m == nil {
			continue
		}
		rest := lower(l[m[1]:])
		vb, body := "", rest
		if loc := reVerified.FindStringIndex(rest); loc != nil {
			vb, body = rest[loc[1]:], rest[:loc[0]]
		}
		out = append(out, "D"+l[m[4]:m[5]]+"\t"+norm(body)+"\t"+norm(vb))
	}
	sort.Strings(out)
	return out
}

// taskRows returns "id\trole|writes|after" per task line of the "Execution plan" section, sorted.
func taskRows(text string) []string {
	var out []string
	for _, l := range strings.Split(section(text, "execution plan"), "\n") {
		if !reTaskRow.MatchString(l) {
			continue
		}
		f := strings.Split(reTaskPre.ReplaceAllString(l, ""), "|")
		id := ""
		if h := strings.Fields(f[0]); len(h) > 0 {
			id = h[0]
		}
		var role, writes, after string
		for _, kv := range f[1:] {
			kv = strings.TrimSpace(kv)
			switch {
			case strings.HasPrefix(kv, "role:"):
				role = strings.TrimLeft(kv[5:], " ")
			case strings.HasPrefix(kv, "writes:"):
				writes = strings.TrimLeft(kv[7:], " ")
			case strings.HasPrefix(kv, "after:"):
				after = strings.TrimLeft(kv[6:], " ")
			}
		}
		out = append(out, id+"\t"+role+"|"+strings.NewReplacer(" ", "", "\t", "").Replace(writes)+"|"+strings.NewReplacer(" ", "", "\t", "").Replace(after))
	}
	sort.Strings(out)
	return out
}

// diffLines prints the differences of two sorted line lists as "  < x" / "  > y" (LCS order, like diff).
func diffLines(a, b []string) []string {
	l := make([][]int, len(a)+1)
	for i := range l {
		l[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else {
				l[i][j] = max(l[i+1][j], l[i][j+1])
			}
		}
	}
	var out, del, add []string
	flush := func() { out, del, add = append(append(out, del...), add...), nil, nil }
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			flush()
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || l[i+1][j] >= l[i][j+1]):
			del = append(del, "  < "+strings.ReplaceAll(a[i], "\t", "|"))
			i++
		default:
			add = append(add, "  > "+strings.ReplaceAll(b[j], "\t", "|"))
			j++
		}
	}
	flush()
	return out
}

// Parity compares the DoD of plan and proposal and, when hasTasks, the task rows of plan and tasks.
// code is 0 equal, 1 differences, 2 no DoD items found (out then holds "plan" or "proposal", the side without items).
func Parity(plan, proposal, tasks string, hasTasks, loose bool) (out []string, code int) {
	a, b := dodItems(plan), dodItems(proposal)
	if len(a) == 0 {
		return []string{"plan"}, 2
	}
	if len(b) == 0 {
		return []string{"proposal"}, 2
	}
	if loose {
		for _, s := range [][]string{a, b} {
			for i, r := range s {
				f := strings.Split(r, "\t")
				s[i] = f[0] + "\t\t" + f[2]
			}
		}
	}
	if d := diffLines(a, b); len(d) > 0 {
		out, code = append(append(out, "DoD differs (< plan, > proposal):"), d...), 1
	}
	if hasTasks {
		if d := diffLines(taskRows(plan), taskRows(tasks)); len(d) > 0 {
			out, code = append(append(out, "tasks differ (< plan, > tasks.md):"), d...), 1
		}
	}
	if code == 0 {
		ok := "parity ok: " + itoa(len(a)) + " DoD item(s)"
		if hasTasks {
			ok += ", tasks match"
		}
		out = append(out, ok)
	}
	return out, code
}
