package setup

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Gap is one line of the prespec-gaps role's answer.
type Gap struct {
	ID, Class, Where, Question, Default, Owner string
}

var reGap = regexp.MustCompile(`^[ \t]*-[ \t]+(G[0-9]+)[ \t]*\|[ \t]*(DECISION|FACT|DEFAULT)[ \t]*\|(.*)$`)

// ParseGaps reads the role's lines and returns the gaps and the problems that make the answer unusable (a DEFAULT without a
// default, a duplicate id, a malformed line). Lines that are not gap lines (SUMMARY, prose) are ignored.
func ParseGaps(text string) (gaps []Gap, problems []string) {
	seen := map[string]bool{}
	for n, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "- G") {
			continue
		}
		m := reGap.FindStringSubmatch(l)
		if m == nil {
			problems = append(problems, fmt.Sprintf("line %d: not '- <Gn> | DECISION|FACT|DEFAULT | <where> | <question> | default: <..> | owner: <..>'", n+1))
			continue
		}
		p := strings.Split(m[3], "|")
		g := Gap{ID: m[1], Class: m[2], Default: "none", Owner: "unknown"}
		if len(p) < 2 {
			problems = append(problems, fmt.Sprintf("line %d: %s needs a where and a question", n+1, g.ID))
			continue
		}
		g.Where, g.Question = strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
		for _, f := range p[2:] {
			f = strings.TrimSpace(f)
			switch {
			case strings.HasPrefix(f, "default:"):
				g.Default = strings.TrimSpace(strings.TrimPrefix(f, "default:"))
			case strings.HasPrefix(f, "owner:"):
				g.Owner = strings.TrimSpace(strings.TrimPrefix(f, "owner:"))
			}
		}
		if seen[g.ID] {
			problems = append(problems, g.ID+": duplicate id")
		}
		seen[g.ID] = true
		if g.Question == "" {
			problems = append(problems, g.ID+": empty question")
		}
		if g.Class == "DEFAULT" && (g.Default == "" || strings.EqualFold(g.Default, "none")) {
			problems = append(problems, g.ID+": a DEFAULT must say what the default is (otherwise it is a DECISION or a FACT)")
		}
		gaps = append(gaps, g)
	}
	return gaps, problems
}

// GapsReport groups the gaps for the analyst: what has to be asked (decisions and facts, in batches of four) and the
// defaults to accept with one yes. It returns the text and whether the answer is usable.
func GapsReport(gaps []Gap, problems []string) (string, bool) {
	var b strings.Builder
	var ask, def []Gap
	for _, g := range gaps {
		if g.Class == "DEFAULT" {
			def = append(def, g)
		} else {
			ask = append(ask, g)
		}
	}
	fmt.Fprintf(&b, "gaps: %d to ask (decision/fact), %d defaults to accept in one question\n", len(ask), len(def))
	for i, g := range ask {
		if i%4 == 0 {
			fmt.Fprintf(&b, "-- question round %d (at most 4 per round)\n", i/4+1)
		}
		fmt.Fprintf(&b, "%s %s (%s) %s [%s] default: %s; owner: %s\n", g.ID, g.Class, g.Where, g.Question, "ask", g.Default, g.Owner)
	}
	if len(def) > 0 {
		b.WriteString("-- accept all defaults? (one question; each accepted default is settled)\n")
		for _, g := range def {
			fmt.Fprintf(&b, "%s (%s) %s -> %s\n", g.ID, g.Where, g.Question, g.Default)
		}
	}
	for _, p := range problems {
		fmt.Fprintln(&b, "problem: "+p)
	}
	return strings.TrimRight(b.String(), "\n"), len(problems) == 0
}

// PrespecGaps reads the role's answer from a file and prints the grouped report; false when the answer is unusable.
func PrespecGaps(file string, out func(string)) bool {
	b, err := os.ReadFile(file)
	if err != nil {
		out("prespec: cannot read " + file)
		return false
	}
	gaps, problems := ParseGaps(string(b))
	if len(gaps) == 0 && len(problems) == 0 {
		problems = append(problems, "no gap lines found")
	}
	rep, ok := GapsReport(gaps, problems)
	out(rep)
	return ok
}
