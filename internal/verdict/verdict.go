// Package verdict parses the output block of the verifier, reviewer and
// security-auditor roles and turns it into a decision. The verifier's
// DOD / FINDINGS / VERIFY / VERDICT format is a frozen contract.
package verdict

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Exit codes of Result.Code.
const (
	CodePass      = 0
	CodeBlock     = 1
	CodeMalformed = 2 // ask the role for the format again, once
	CodeRefused   = 3 // well formed, but the recorder refused it
)

// Malformed kinds: format = shape only (re-ask is enough), semantic = the
// content contradicts itself or lacks evidence (re-asking is not a fix).
const (
	KindFormat   = "format"
	KindSemantic = "semantic"
)

// Options select the role and the extra checks.
type Options struct {
	Role         string // verifier (default), reviewer, security-auditor
	ProposalPath string // verifier only: every DoD item of it must be judged
	NoNormalize  bool   // PARSE_NORMALIZE=0
	// Accepted says whether a person accepted DoD item id of the proposal
	// (dod-check accepted). Nil means no acceptance exists.
	Accepted func(proposal, id string) bool
}

// Result is the decision. Kind and Reason are set only when malformed.
type Result struct {
	Role, Verdict string // Verdict is PASS or BLOCK, empty when malformed
	Kind, Reason  string
	Summary       string // the one summary line of a well-formed block
}

// Code is the exit code: 0 PASS, 1 BLOCK, 2 malformed.
func (r Result) Code() int {
	switch {
	case r.Kind != "":
		return CodeMalformed
	case r.Verdict == "PASS":
		return CodePass
	}
	return CodeBlock
}

// Line is the one line the command prints.
func (r Result) Line() string {
	if r.Kind != "" {
		return fmt.Sprintf("MALFORMED[%s]: %s", r.Kind, r.Reason)
	}
	return r.Summary
}

// Recorder stores a well-formed verdict in the workflow state. P1-07 (sdd)
// sets it; this package does not import sdd. refused is true when the state
// machine says no (stale, duplicate, unknown run ...); msg is the reason.
type Recorder interface {
	Record(role, verdict, source, run, task string) (msg string, refused bool)
}

// Parse checks a verifier block with the default options.
func Parse(text string) Result { return ParseWith(text, Options{}) }

func bad(kind, f string, a ...any) Result {
	return Result{Kind: kind, Reason: fmt.Sprintf(f, a...)}
}

var (
	reHead    = regexp.MustCompile(`^[ \t]*[*_]+(DOD|FINDINGS|VERIFY|SUMMARY)[*_]*:?[ \t]*$`)
	reVerdict = regexp.MustCompile("^[ \t]*[*_]*VERDICT[*_]*[ \t]*:?[*_ \t]*:?[ \t]*[*_`]*[A-Za-z]+[*_`]*[ \t]*$")
	reVPre    = regexp.MustCompile("^[ \t]*[*_]*VERDICT[*_]*[ \t]*:?[*_ \t]*:?[ \t]*[*_`]*")
	reVPost   = regexp.MustCompile("[*_`]*[ \t]*$")
	rePF      = regexp.MustCompile(`\|[ \t]*[*_]*(pass|fail|PASS|FAIL|Pass|Fail)[*_]*[ \t]*(\||$)`)
	reFind    = regexp.MustCompile(`^[ \t]*([-*]|[0-9]+[.)])[ \t]+[*_]*(confirmed|plausible|Confirmed|Plausible)[*_]*[ \t]*\|`)
	reFPre    = regexp.MustCompile(`^[ \t]*([-*]|[0-9]+[.)])[ \t]+[*_]*`)
	reStar    = regexp.MustCompile(`^[ \t]*[*][ \t]+`)
	reNum     = regexp.MustCompile(`^[ \t]*[0-9]+[.)][ \t]+`)
	reBullet  = regexp.MustCompile(`^[ \t]*([-*]|[0-9]+[.)])[ \t]+`)
	reEv      = regexp.MustCompile(`^.*\|[ \t]*PASS[ \t]*\|?[ \t]*`)
	reD       = regexp.MustCompile(`D[0-9]+`)
	reRes     = regexp.MustCompile(`result:[ \t]*(green|red)`)
	reRed     = regexp.MustCompile(`result:[ \t]*red`)
	reWant    = regexp.MustCompile(`^[ \t]*([-*][ \t]+)?[*_]*D[0-9]+[*_]*[.:)]`)
)

func init() {
	for _, r := range []*regexp.Regexp{reHead, reVerdict, reVPre, reVPost} {
		r.Longest()
	}
}

// Normalize applies the deterministic rules that cannot change a judgement:
// CR line ends, markdown emphasis around a marker or a token, lower-case
// tokens, `*` and `1.` bullets as `-`. Nothing is inferred.
func Normalize(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for i, l := range lines {
		switch {
		case reHead.MatchString(l):
			l = strings.Map(func(r rune) rune {
				if strings.ContainsRune(" \t*_:", r) {
					return -1
				}
				return r
			}, l)
		case reVerdict.MatchString(l):
			v := reVPost.ReplaceAllString(reVPre.ReplaceAllString(l, ""), "")
			l = "VERDICT: " + strings.ToUpper(v)
		case rePF.MatchString(l):
			f := strings.Split(l, "|")
			for j := 1; j < len(f); j++ {
				t := strings.ToUpper(strings.Trim(f[j], " \t*_"))
				if t == "PASS" || t == "FAIL" {
					f[j] = " " + t + " "
					break
				}
			}
			l = strings.Join(f, "|")
		}
		if reFind.MatchString(l) {
			t := reFPre.ReplaceAllString(l, "")
			k := strings.Index(t, "|")
			w := strings.ToUpper(strings.TrimRight(t[:k], "*_ \t"))
			l = "- " + w + " " + t[k:]
		}
		l = reStar.ReplaceAllString(l, "- ")
		lines[i] = reNum.ReplaceAllString(l, "- ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// ParseWith checks the block of o.Role.
func ParseWith(text string, o Options) Result {
	if !o.NoNormalize {
		text = Normalize(text)
	}
	role := o.Role
	if role == "" {
		role = "verifier"
	}
	var block []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimLeft(l, " \t\n\v\f\r"), "```") {
			block = append(block, l)
		}
	}
	switch role {
	case "verifier":
		return verifier(block, o)
	case "reviewer", "security-auditor":
		return auditor(block, role)
	}
	return bad(KindFormat, "unknown --role '%s'", role)
}

func find(block []string, re string) int { // 1-based line of the first match, 0 if none
	r := regexp.MustCompile(re)
	for i, l := range block {
		if r.MatchString(l) {
			return i + 1
		}
	}
	return 0
}

func count(block []string, re string) int {
	r := regexp.MustCompile(re)
	n := 0
	for _, l := range block {
		if r.MatchString(l) {
			n++
		}
	}
	return n
}

// section returns the bullet items between two marker lines, as "- item".
func section(block []string, a, b int) []string {
	var out []string
	for _, l := range block[a : b-1] {
		if reBullet.MatchString(l) {
			out = append(out, reBullet.ReplaceAllString(l, "- "))
		}
	}
	return out
}

func verdictOf(l string) string {
	return strings.TrimRight(strings.TrimLeft(strings.TrimPrefix(l, "VERDICT:"), " \t\n\v\f\r"), " \t\n\v\f\r")
}

var noEvidence = map[string]bool{"": true, "-": true, "n/a": true, "na": true, "manual": true, "manual.": true,
	"post-deploy": true, "postdeploy": true, "pending": true, "tbd": true, "todo": true, "none": true}

func verifier(block []string, o Options) Result {
	ld, lf, lv, lw := find(block, `^DOD[ \t]*$`), find(block, `^FINDINGS[ \t]*$`), find(block, `^VERIFY[ \t]*$`), find(block, `^VERDICT:`)
	if ld == 0 || lf == 0 || lv == 0 || lw == 0 {
		return bad(KindFormat, "missing a DOD / FINDINGS / VERIFY / VERDICT marker")
	}
	if !(ld < lf && lf < lv && lv < lw) {
		return bad(KindFormat, "sections are out of order")
	}
	dod, finds, ver := section(block, ld, lf), section(block, lf, lv), section(block, lv, lw)
	if len(dod) == 0 {
		return bad(KindFormat, "DOD has no items")
	}
	pass, fail, accepted := 0, 0, 0
	for _, l := range dod {
		switch {
		case strings.Contains(l, "| PASS |") || strings.HasSuffix(l, "| PASS") || strings.Contains(l, "|PASS|"):
			// A PASS carries evidence: manual, post-deploy, n/a or nothing never counts.
			ev := strings.ToLower(strings.TrimRight(reEv.ReplaceAllString(l, ""), " \t"))
			if ev == "accepted" || strings.HasPrefix(ev, "accepted:") || strings.HasPrefix(ev, "accepted ") {
				id := reD.FindString(l)
				if o.ProposalPath == "" || id == "" || o.Accepted == nil || !fileExists(o.ProposalPath) || !o.Accepted(o.ProposalPath, id) {
					if id == "" {
						id = "this item"
					}
					return bad(KindSemantic, "PASS claims an acceptance, but no person's acceptance is recorded for %s as it now reads (dod-check.sh accept; needs --proposal): %s", id, l)
				}
				accepted++
			}
			if noEvidence[ev] || strings.Contains(ev, "not verified") {
				return bad(KindSemantic, "PASS without evidence (a manual, post-deploy or empty item is not a pass; use FAIL, or give file:line / test name): %s", l)
			}
			pass++
		case strings.Contains(l, "| FAIL |") || strings.HasSuffix(l, "| FAIL") || strings.Contains(l, "|FAIL|"):
			fail++
		default:
			return bad(KindFormat, "DoD line without PASS|FAIL: %s", l)
		}
	}
	conf, plaus := 0, 0
	for _, l := range finds {
		switch {
		case strings.HasPrefix(l, "- CONFIRMED") && wsThenPipe(l[len("- CONFIRMED"):]):
			conf++
		case strings.HasPrefix(l, "- PLAUSIBLE") && wsThenPipe(l[len("- PLAUSIBLE"):]):
			plaus++
		case strings.HasPrefix(l, "- none"), strings.HasPrefix(l, "- None"):
		default:
			return bad(KindFormat, "finding without CONFIRMED|PLAUSIBLE: %s", l)
		}
	}
	if len(ver) == 0 {
		return bad(KindFormat, "VERIFY has no command line")
	}
	vj := strings.Join(ver, "\n")
	if !reRes.MatchString(vj) {
		return bad(KindFormat, "VERIFY line lacks 'result: green|red'")
	}
	v := verdictOf(block[lw-1])
	if v != "PASS" && v != "BLOCK" {
		return bad(KindFormat, "VERDICT must be PASS or BLOCK, got '%s'", v)
	}
	expect := "PASS"
	if fail > 0 || conf > 0 {
		expect = "BLOCK"
	}
	if v != expect {
		return bad(KindSemantic, "VERDICT is %s but %d FAIL item(s) and %d CONFIRMED finding(s) require %s", v, fail, conf, expect)
	}
	if v == "PASS" && reRed.MatchString(vj) {
		return bad(KindSemantic, "VERDICT is PASS but a VERIFY command is red")
	}
	if o.ProposalPath != "" {
		b, err := os.ReadFile(o.ProposalPath)
		if err != nil {
			return bad(KindFormat, "proposal not found: %s", o.ProposalPath)
		}
		if want := count(strings.Split(string(b), "\n"), reWant.String()); want != 0 && want != pass+fail {
			return bad(KindSemantic, "proposal lists %d DoD items, verifier judged %d (skipped items)", want, pass+fail)
		}
	}
	s := fmt.Sprintf("VERDICT=%s dod_pass=%d dod_fail=%d confirmed=%d plausible=%d", v, pass, fail, conf, plaus)
	if accepted > 0 {
		s += fmt.Sprintf(" accepted=%d", accepted)
	}
	return Result{Role: "verifier", Verdict: v, Summary: s}
}

func wsThenPipe(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\t') && strings.Contains(s[1:], "|")
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func auditor(block []string, role string) Result {
	sevs, blocking := []string{"BLOCKER", "MAJOR", "MINOR"}, 1
	sumre := `^SUMMARY:[ \t]*([0-9]+) blockers?, ([0-9]+) major, ([0-9]+) minor[ \t]*$`
	if role != "reviewer" {
		sevs, blocking = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}, 2
		sumre = `^SUMMARY:[ \t]*([0-9]+) critical, ([0-9]+) high, ([0-9]+) medium, ([0-9]+) low[ \t]*$`
	}
	var counts []string
	total, nblock := 0, 0
	for i, sv := range sevs {
		c := count(block, `^[ \t]*([-*]|[0-9]+[.)])?[ \t]*[*_]*`+sv+`[*_]*[ \t]*\|`)
		counts = append(counts, strconv.Itoa(c))
		total += c
		if i < blocking {
			nblock += c
		}
	}
	if count(block, `^SUMMARY:`) != 1 {
		return bad(KindFormat, "%s output needs exactly one SUMMARY line", role)
	}
	m := regexp.MustCompile(sumre).FindStringSubmatch(block[find(block, `^SUMMARY:`)-1])
	if m == nil {
		return bad(KindFormat, "SUMMARY line is not in the %s format", role)
	}
	if said, want := strings.Join(m[1:], " "), strings.Join(counts, " "); said != want {
		return bad(KindSemantic, "SUMMARY says '%s' but the findings count '%s'", said, want)
	}
	if role == "security-auditor" {
		if nof := count(block, `^[ \t]*NO FINDINGS`); !(total == 0 && nof >= 1) && !(total > 0 && nof == 0) {
			return bad(KindFormat, "NO FINDINGS must appear exactly when there are no findings")
		}
	}
	if count(block, `^[ \t]*[*_]*VERDICT`) != 1 {
		return bad(KindFormat, "%s output needs exactly one VERDICT line", role)
	}
	ls, lv := find(block, `^SUMMARY:`), find(block, `^VERDICT:`)
	if lv == 0 || ls >= lv {
		return bad(KindFormat, "VERDICT must come after SUMMARY")
	}
	v := verdictOf(block[lv-1])
	if v != "PASS" && v != "BLOCK" {
		return bad(KindFormat, "VERDICT must be PASS or BLOCK, got '%s'", v)
	}
	expect := "PASS"
	if nblock > 0 {
		expect = "BLOCK"
	}
	if v != expect {
		return bad(KindSemantic, "VERDICT is %s but %d blocking finding(s) require %s", v, nblock, expect)
	}
	return Result{Role: role, Verdict: v, Summary: fmt.Sprintf("VERDICT=%s role=%s findings=%d blocking=%d", v, role, total, nblock)}
}
