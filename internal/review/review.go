// Package review is the deterministic half of the spec review: it keeps the
// evidence honest (PASS bound to the document's content hash, fresh isolation
// only, bounded rounds, acceptances keyed on the finding text). The review
// itself is done by fresh reviewers outside this code.
package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/lint"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/store"
)

// Error carries the exit code of a refusal: 1 refused by a rule, 2 usage or malformed input, 3 round cap reached.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return "review: " + e.Msg }

// errKeep is returned from a store.Update callback to leave the state file as it is.
var errKeep = errors.New("keep")

func fail(code int, f string, a ...any) *Error { return &Error{code, fmt.Sprintf(f, a...)} }

// Ctx is what a review needs from the project.
type Ctx struct {
	Root, KitDir, SpecsDir string
	MaxRounds              int
	JiraUser               string
	// IdempotentBegin (REVIEW_BEGIN_IDEMPOTENT=1): begin on content that already has its evidence, or an open round, uses no round.
	IdempotentBegin bool
	// RequireLint (REVIEW_REQUIRE_LINT=1): begin refuses while `review lint` has errors, so no model round is spent on mechanical mistakes.
	RequireLint bool
	Lint        lint.Config
}

// Load reads the settings of the project at root.
func Load(root string) Ctx {
	s, p, _ := project.Load(root)
	c := Ctx{Root: root, KitDir: p.KitDir, SpecsDir: s.String("SPECS_DIR"), MaxRounds: 4, JiraUser: s.String("JIRA_USER")}
	if c.SpecsDir == "" {
		c.SpecsDir = "specs"
	}
	if n := s.Int("SPEC_REVIEW_MAX_ROUNDS", 4); n >= 0 && regexp.MustCompile(`^[0-9]+$`).MatchString(strings.TrimSpace(s.String("SPEC_REVIEW_MAX_ROUNDS"))) {
		c.MaxRounds = n
	}
	if c.JiraUser == "" {
		c.JiraUser = os.Getenv("JIRA_USER")
	}
	c.IdempotentBegin = s.Bool("REVIEW_BEGIN_IDEMPOTENT")
	c.RequireLint = s.Bool("REVIEW_REQUIRE_LINT")
	c.Lint = lint.Config{RefPrefixes: s.List("LINT_REF_PREFIXES"), GatePrefix: s.String("LINT_GATE_PREFIX"), Withdrawn: lower(s.List("LINT_WITHDRAWN_WORDS")),
		MarkerWords: lower(s.List("LINT_MARKER_WORDS")), ClosedWords: lower(s.List("LINT_CLOSED_WORDS"))}
	return c
}

func lower(l []string) []string {
	for i := range l {
		l[i] = strings.ToLower(l[i])
	}
	return l
}

// LintTarget runs the mechanical checks on the document of id and returns the problems.
func (c Ctx) LintTarget(id, override string) ([]lint.Problem, error) {
	t, err := c.target(id, override)
	if err != nil {
		return nil, err
	}
	return lint.Run(t, c.Lint)
}

func (c Ctx) work(id string) string      { return filepath.Join(c.Root, c.KitDir, "work", "review", id) }
func (c Ctx) stateFile(id string) string { return filepath.Join(c.work(id), "state.json") }

var reName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// OkName reports whether s is usable as a review id or artifact name.
func OkName(s string) bool { return reName.MatchString(s) }

// Finding is one reviewer finding.
type Finding struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Evidence    string `json:"evidence"`
	Disposition string `json:"disposition"`
}

// Evidence is the recorded result of a round.
type Evidence struct {
	Verdict     string    `json:"verdict"`
	Isolation   string    `json:"isolation"`
	ContentHash string    `json:"contentHash"`
	Round       int       `json:"round"`
	At          string    `json:"at"`
	Findings    []Finding `json:"findings"`
}

// State is <project-dir>/work/review/<id>/state.json.
type State struct {
	ID          string    `json:"id,omitempty"`
	Target      string    `json:"target,omitempty"`
	ContentHash string    `json:"contentHash,omitempty"`
	Round       int       `json:"round"`
	Pending     bool      `json:"pending"`
	BegunAt     string    `json:"begunAt,omitempty"`
	FinalUsed   bool      `json:"finalUsed,omitempty"`
	Evidence    *Evidence `json:"evidence,omitempty"`
}

func readState(path string) (State, bool) {
	var st State
	b, err := os.ReadFile(path)
	if err != nil {
		return st, false
	}
	if json.Unmarshal(b, &st) != nil {
		return State{}, true
	}
	return st, true
}

func compact(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (c Ctx) target(id, override string) (string, error) {
	t := override
	if t == "" {
		if st, _ := readState(c.stateFile(id)); st.Target != "" {
			t = st.Target
		}
	}
	if t == "" {
		t = c.SpecsDir + "/" + id
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(c.Root, t)
	}
	if _, err := os.Stat(t); err != nil {
		return "", fail(2, "no document at %s", t)
	}
	return t, nil
}

// Hash is the contentHash of a document (a directory or one file): sha256 over
// the sorted lines "<sha256 of file>  ./<relative path>". A directory excludes
// spec.meta.json, change.json, acceptances.jsonl, .DS_Store and the top-level changes/.
func Hash(t string) (string, error) {
	var d string
	var list []string
	if st, err := os.Stat(t); err == nil && st.IsDir() {
		d = t
		filepath.WalkDir(t, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(t, p)
			rel = filepath.ToSlash(rel)
			switch e.Name() {
			case "spec.meta.json", "change.json", "acceptances.jsonl", ".DS_Store":
				return nil
			}
			if strings.HasPrefix(rel, "changes/") {
				return nil
			}
			list = append(list, "./"+rel)
			return nil
		})
	} else {
		d, list = filepath.Dir(t), []string{"./" + filepath.Base(t)}
	}
	if len(list) == 0 {
		return "", fail(2, "%s has no files to hash", t)
	}
	sort.Strings(list)
	var sb strings.Builder
	for _, f := range list {
		b, err := os.ReadFile(filepath.Join(d, filepath.FromSlash(f)))
		if err != nil {
			return "", fail(2, "cannot read %s", f)
		}
		fmt.Fprintf(&sb, "%s  %s\n", sum(b), f)
	}
	return sum([]byte(sb.String())), nil
}

func accFile(t string) string {
	if st, err := os.Stat(t); err == nil && st.IsDir() {
		return filepath.Join(t, "acceptances.jsonl")
	}
	return t + ".acceptances.jsonl"
}

// fhash hashes what a finding says (id, severity, evidence); the disposition is not part of it.
func fhash(f Finding) string {
	return sum([]byte(compact([]string{f.ID, f.Severity, f.Evidence}) + "\n"))
}

func acceptedOK(file, id, h string) bool {
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		var raw map[string]any
		if json.Unmarshal([]byte(l), &raw) == nil && raw["finding"] == id && raw["finding_sha256"] == h {
			return true
		}
	}
	return false
}

// blocking lists the BLOCKER/MAJOR findings still open (open, or accepted without a valid acceptance).
func blocking(fs []Finding, accFile string) []string {
	var out []string
	for _, f := range fs {
		if f.Severity != "BLOCKER" && f.Severity != "MAJOR" {
			continue
		}
		switch f.Disposition {
		case "fixed":
		case "accepted":
			if !acceptedOK(accFile, f.ID, fhash(f)) {
				out = append(out, f.ID)
			}
		default:
			out = append(out, f.ID)
		}
	}
	return out
}

var reLine = regexp.MustCompile(`^[ \t]*[-*][ \t]+[A-Za-z0-9._-]+[ \t]*\|`)

// LoadFindings reads and validates a findings file: a JSON array or the spec-reviewer lines.
func LoadFindings(path string) ([]Finding, error) {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return nil, fail(2, "findings file '%s' not found", path)
	}
	b, _ := os.ReadFile(path)
	raw := strings.TrimPrefix(string(b), "\xef\xbb\xbf")
	var fs []Finding
	lead := strings.TrimSpace(raw)
	if strings.HasPrefix(lead, "[") || strings.HasPrefix(lead, "{") {
		if json.Unmarshal([]byte(raw), &fs) != nil && !json.Valid([]byte(raw)) {
			return nil, fail(2, "findings are not valid JSON")
		}
	} else {
		bad := false
		for _, l := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
			switch {
			case reLine.MatchString(l):
				l = strings.TrimLeft(l, " \t")[1:]
				p := strings.Split(strings.TrimLeft(l, " \t"), "|")
				if len(p) < 4 {
					bad = true
					continue
				}
				t := strings.TrimSpace(strings.Join(p[3:], "|"))
				fs = append(fs, Finding{strings.TrimSpace(p[0]), strings.ToUpper(strings.TrimSpace(p[1])), strings.TrimSpace(p[2]) + ": " + t, "open"})
			case regexp.MustCompile(`^[ \t]*[-*][ \t]`).MatchString(l):
				bad = true
			}
		}
		if bad {
			return nil, fail(2, "findings lines must read '- <id> | <BLOCKER|MAJOR|MINOR> | <where> | <text>'")
		}
	}
	seen := map[string]bool{}
	ok := true
	for _, f := range fs {
		ok = ok && f.ID != "" && !seen[f.ID] && strings.TrimSpace(f.Evidence) != "" &&
			(f.Severity == "BLOCKER" || f.Severity == "MAJOR" || f.Severity == "MINOR") &&
			(f.Disposition == "open" || f.Disposition == "fixed" || f.Disposition == "accepted")
		seen[f.ID] = true
	}
	if !ok || (strings.HasPrefix(lead, "{") && fs == nil) {
		return nil, fail(2, "malformed findings: need an array of {id (unique), severity BLOCKER|MAJOR|MINOR, evidence (non-empty), disposition open|fixed|accepted}")
	}
	if fs == nil {
		fs = []Finding{}
	}
	return fs, nil
}

// Begin opens a review round and returns the line to print. Past MaxRounds it
// refuses with code 3; final opens the one verification round after the cap.
func (c Ctx) Begin(id, target string, final bool) (string, error) {
	t, err := c.target(id, target)
	if err != nil {
		return "", err
	}
	h, err := Hash(t)
	if err != nil {
		return "", err
	}
	if c.RequireLint && !final {
		ps, err := lint.Run(t, c.Lint)
		if err != nil {
			return "", fail(2, "lint: %v", err)
		}
		if n := lint.Errors(ps); n > 0 {
			var sb strings.Builder
			for i, p := range ps {
				if p.Severity == "error" && i < 25 {
					sb.WriteString("\n  " + p.String())
				}
			}
			return "", fail(1, "REVIEW_REQUIRE_LINT: %d mechanical error(s) in '%s'; fix them (no model round is spent on these) and run begin again:%s", n, id, sb.String())
		}
	}
	rel := t
	if r, err := filepath.Rel(c.Root, t); err == nil && !strings.HasPrefix(r, "..") && !filepath.IsAbs(r) {
		rel = filepath.ToSlash(r)
	}
	var msg string
	err = store.Update(c.stateFile(id), func(old []byte) ([]byte, error) {
		var st State
		json.Unmarshal(old, &st)
		prev := st.Round
		// Same content as the round already open or already reviewed: another round would only sample the same
		// document again. A NOT_RUN round learned nothing, so it may be repeated.
		if c.IdempotentBegin && !final && prev > 0 && st.ContentHash == h {
			switch {
			case st.Pending:
				msg = fmt.Sprintf("review: round %d of '%s' is already open at %s (unchanged, no round used)", prev, id, h)
				return nil, errKeep
			case st.Evidence != nil && st.Evidence.ContentHash == h && st.Evidence.Verdict != "NOT_RUN":
				msg = fmt.Sprintf("review: '%s' is unchanged since round %d (%s, %s); its evidence stands, no round used. Change the document before a new round", id, prev, st.Evidence.Verdict, h)
				return nil, errKeep
			}
		}
		if final {
			if prev < c.MaxRounds {
				return nil, fail(1, "--final is only for after the round cap (%d of %d used); open a normal round", prev, c.MaxRounds)
			}
			if st.FinalUsed {
				return nil, fail(3, "the final verification round of '%s' is already used. A person decides (rework by hand or raise SPEC_REVIEW_MAX_ROUNDS).", id)
			}
		} else if prev >= c.MaxRounds {
			return nil, fail(3, "round cap reached for '%s' (%d of SPEC_REVIEW_MAX_ROUNDS=%d used). No further automatic round: a person decides (rework the document by hand or raise SPEC_REVIEW_MAX_ROUNDS).", id, prev, c.MaxRounds)
		}
		st.ID, st.Target, st.ContentHash, st.Pending, st.BegunAt = id, rel, h, true, now()
		if final {
			st.FinalUsed = true
			msg = fmt.Sprintf("review: final verification round opened for '%s' at %s (outside the cap of %d; the only one)", id, h, c.MaxRounds)
		} else {
			st.Round = prev + 1
			msg = fmt.Sprintf("review: round %d of %d opened for '%s' at %s", st.Round, c.MaxRounds, id, h)
		}
		return []byte(compact(st) + "\n"), nil
	})
	if err == errKeep {
		err = nil
	}
	return msg, err
}

// Record stores the round's evidence, bound to the hash seen at Begin.
func (c Ctx) Record(id, verdict, isolation, findings string) (string, error) {
	switch verdict {
	case "PASS", "BLOCK", "PARTIAL", "NOT_RUN":
	default:
		return "", fail(2, "--verdict must be PASS|BLOCK|PARTIAL|NOT_RUN")
	}
	if isolation != "fresh" && isolation != "none" {
		return "", fail(2, "--isolation must be fresh|none")
	}
	if verdict == "PASS" && isolation == "none" {
		return "", fail(2, "isolation=none can never be PASS (a review in the author's context is not a review); record NOT_RUN or run a fresh reviewer")
	}
	sf := c.stateFile(id)
	if _, ok := readState(sf); !ok {
		return "", fail(2, "no review round open for '%s': run review begin first", id)
	}
	fj := []Finding{}
	switch {
	case findings != "":
		var err error
		if fj, err = LoadFindings(findings); err != nil {
			return "", err
		}
	case verdict != "NOT_RUN":
		return "", fail(2, "--findings FILE is required (an empty array [] is a review with no findings)")
	}
	var msg string
	err := store.Update(sf, func(old []byte) ([]byte, error) {
		var st State
		json.Unmarshal(old, &st)
		if !st.Pending {
			return nil, fail(1, "round %d of '%s' already has its evidence; begin a new round", st.Round, id)
		}
		t, err := c.target(id, "")
		if err != nil {
			return nil, err
		}
		if bl := blocking(fj, accFile(t)); verdict == "PASS" && len(bl) > 0 {
			return nil, fail(2, "PASS contradicts the findings: BLOCKER/MAJOR still open or accepted without a valid acceptance: %s", strings.Join(bl, " "))
		}
		st.Pending = false
		st.Evidence = &Evidence{verdict, isolation, st.ContentHash, st.Round, now(), fj}
		msg = fmt.Sprintf("review: %s (%s) recorded for '%s' round %d, bound to %s", verdict, isolation, id, st.Round, st.ContentHash)
		return []byte(compact(st) + "\n"), nil
	})
	return msg, err
}

// Result is the standing of a document's review. State is PASS, stale,
// NOT_RUN (no evidence, not fresh, or recorded NOT_RUN), BLOCK or PARTIAL (the
// latest verdict) or OPEN (PASS but a BLOCKER/MAJOR is open). Hash is the
// document's current content hash (the fingerprint), Reviewed the one the
// evidence is bound to. Reason is the text `review check` prints.
type Result struct {
	State, Reason, Hash, Reviewed string
	Round                         int
}

// OK is true only for a fresh PASS on the current content.
func (r Result) OK() bool { return r.State == "PASS" }

// Status answers "PASS on this spec version" for the review id spec (project root root).
func Status(root, spec string) Result { return Load(root).Check(spec) }

// Check evaluates the latest evidence of id against the document as it is now.
func (c Ctx) Check(id string) Result {
	r := Result{State: "NOT_RUN"}
	no := func(state, f string, a ...any) Result {
		r.State, r.Reason = state, "NOT approved: "+fmt.Sprintf(f, a...)
		return r
	}
	if !OkName(id) {
		return no("NOT_RUN", "no review evidence for '%s'", id)
	}
	st, _ := readState(c.stateFile(id))
	if st.Evidence == nil || st.Evidence.Verdict == "" {
		return no("NOT_RUN", "no review evidence for '%s'", id)
	}
	ev := st.Evidence
	r.Reviewed, r.Round = ev.ContentHash, ev.Round
	t, err := c.target(id, "")
	if err != nil {
		return no("stale", "the document is gone")
	}
	cur, err := Hash(t)
	if err != nil {
		return no("stale", "cannot hash the document")
	}
	r.Hash = cur
	switch {
	case ev.ContentHash != cur:
		return no("stale", "stale: the document changed after the review (reviewed %s, now %s); begin a new round", ev.ContentHash, cur)
	case ev.Isolation != "fresh":
		return no("NOT_RUN", "isolation is '%s', not fresh", ev.Isolation)
	case ev.Verdict != "PASS":
		return no(ev.Verdict, "latest verdict is %s", ev.Verdict)
	}
	if bl := blocking(ev.Findings, accFile(t)); len(bl) > 0 {
		return no("OPEN", "BLOCKER/MAJOR still open: %s", strings.Join(bl, " "))
	}
	r.State, r.Reason = "PASS", fmt.Sprintf("approved '%s' (PASS, fresh, round %d, %s)", id, ev.Round, cur)
	return r
}

// Accept records that a person accepts one finding of the latest evidence; returns the line to print.
func (c Ctx) Accept(id, fid, who, why string) (string, error) {
	if strings.TrimSpace(who) == "" {
		who = c.JiraUser
	}
	if strings.TrimSpace(who) == "" || strings.TrimSpace(why) == "" {
		return "", fail(2, "accept needs --why \"<reason>\" and a person: --who or JIRA_USER in sdkit.local.env (nothing is accepted by default)")
	}
	st, ok := readState(c.stateFile(id))
	if !ok {
		return "", fail(2, "no review evidence for '%s'", id)
	}
	var f *Finding
	if st.Evidence != nil {
		for i := range st.Evidence.Findings {
			if st.Evidence.Findings[i].ID == fid {
				f = &st.Evidence.Findings[i]
				break
			}
		}
	}
	if f == nil {
		return "", fail(1, "finding '%s' is not in the latest evidence of '%s'", fid, id)
	}
	t, err := c.target(id, "")
	if err != nil {
		return "", err
	}
	af := accFile(t)
	line := compact(struct {
		V           int    `json:"v"`
		Ts          string `json:"ts"`
		ID          string `json:"id"`
		Finding     string `json:"finding"`
		FindingSha  string `json:"finding_sha256"`
		Severity    string `json:"severity"`
		Who         string `json:"who"`
		Why         string `json:"why"`
		ContentHash string `json:"contentHash"`
	}{1, now(), id, fid, fhash(*f), f.Severity, who, why, st.Evidence.ContentHash})
	unlock, err := store.Lock(af)
	if err != nil {
		return "", fail(2, "cannot lock %s", af)
	}
	defer unlock()
	if err := store.AppendLine(af, line); err != nil {
		return "", fail(2, "cannot write %s", af)
	}
	return fmt.Sprintf("review: finding %s accepted by %s (recorded in %s); this binds the finding as written now", fid, who, af), nil
}

// Rubric is a resolved review.d/<artifact>.env.
type Rubric struct {
	Artifact, Source string
	Reviewers        []string
	Standards        [][2]string // reviewer, standard, in file order
	Context          []string
}

// Plan resolves the rubric of an artifact: <project-dir>/review.d/<artifact>.env, else the kit's template.
func (c Ctx) Plan(art string) (*Rubric, error) {
	src := filepath.Join(c.Root, c.KitDir, "review.d", art+".env")
	data, err := os.ReadFile(src)
	if err != nil {
		src = "templates/review.d/" + art + ".env (kit)"
		if data, err = project.KitFile("templates/review.d/" + art + ".env"); err != nil {
			return nil, fail(2, "no rubric for '%s' (looked for %s and the kit's templates/review.d/%s.env)", art, filepath.Join(c.Root, c.KitDir, "review.d", art+".env"), art)
		}
	}
	s := envfile.New()
	s.Quiet = true
	s.LoadBytes(art+".env", data)
	get := func(k string) string { v, _ := s.Raw(k); return v }
	r := &Rubric{Artifact: art, Source: src, Reviewers: strings.Fields(get("REVIEWERS")), Context: strings.Fields(get("CONTEXT"))}
	if len(r.Reviewers) == 0 {
		return nil, fail(2, "%s sets no REVIEWERS", src)
	}
	for _, p := range strings.Fields(get("STANDARDS")) {
		i := strings.Index(p, "=")
		if i < 0 || i == len(p)-1 {
			return nil, fail(2, "%s: STANDARDS entry '%s' is not reviewer=standard", src, p)
		}
		found := false
		for _, rv := range r.Reviewers {
			found = found || rv == p[:i]
		}
		if !found {
			return nil, fail(2, "%s: STANDARDS names '%s', which is not in REVIEWERS", src, p[:i])
		}
		r.Standards = append(r.Standards, [2]string{p[:i], strings.SplitN(p[i+1:], "=", 2)[0]})
	}
	return r, nil
}

// Text is the plain form of the rubric.
func (r *Rubric) Text() string {
	or := func(l []string) string {
		if len(l) == 0 {
			return "none"
		}
		return strings.Join(l, " ")
	}
	var st []string
	for _, p := range r.Standards {
		st = append(st, p[0]+"="+p[1])
	}
	return fmt.Sprintf("artifact:  %s\nsource:    %s\nreviewers: %s\nstandards: %s\ncontext:   %s\n", r.Artifact, r.Source, strings.Join(r.Reviewers, " "), or(st), or(r.Context))
}

// JSON is the indented JSON form, keys in the order the bash printed them.
func (r *Rubric) JSON() string {
	q := func(s string) string { return compact(s) }
	list := func(l []string, ind string) string {
		if len(l) == 0 {
			return "[]"
		}
		var o []string
		for _, s := range l {
			o = append(o, ind+"  "+q(s))
		}
		return "[\n" + strings.Join(o, ",\n") + "\n" + ind + "]"
	}
	std := "{}"
	if len(r.Standards) > 0 {
		var o []string
		for _, p := range r.Standards {
			o = append(o, "    "+q(p[0])+": "+q(p[1]))
		}
		std = "{\n" + strings.Join(o, ",\n") + "\n  }"
	}
	return fmt.Sprintf("{\n  \"artifact\": %s,\n  \"source\": %s,\n  \"reviewers\": %s,\n  \"standards\": %s,\n  \"context\": %s\n}\n",
		q(r.Artifact), q(r.Source), list(r.Reviewers, "  "), std, list(r.Context, "  "))
}

// IsError unwraps an *Error.
func IsError(err error) (*Error, bool) { var e *Error; return e, errors.As(err, &e) }

// Target resolves the document of id (override, else the target remembered at begin, else <SPECS_DIR>/<id>).
func (c Ctx) Target(id, override string) (string, error) { return c.target(id, override) }
