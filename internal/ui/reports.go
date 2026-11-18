package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/report"
	"github.com/wecii/sdkit/internal/state"
)

// A report is filed under the host's session id and says nothing about which spec the session worked on. The panel starts the
// sessions of a spec, so it is the one place that knows both: it writes down (spec, action, session, times) for every run, and a
// report is linked to a spec only through that record. A session started anywhere else stays unlinked; nothing is guessed from
// times or branch names.

// RunRecord is one panel run as the ledger holds it.
type RunRecord struct {
	Session string `json:"session"`
	Spec    string `json:"spec"`
	Action  string `json:"action"`
	Start   string `json:"start"`
	End     string `json:"end,omitempty"`
	Outcome string `json:"outcome,omitempty"` // done | failed | stopped; empty while it runs or when the panel died first
}

func runsPath(root string) string {
	_, pj, _ := project.Load(root)
	return filepath.Join(pj.Dir(), "work", "ui", "runs.jsonl")
}

// RecordRun appends one line; a start line and an end line of the same session fold into one RunRecord when read.
func RecordRun(root string, r RunRecord) error {
	p := runsPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadRuns folds the ledger by session, oldest first.
func ReadRuns(root string) []RunRecord {
	b, _ := os.ReadFile(runsPath(root))
	idx, out := map[string]int{}, []RunRecord{}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		var r RunRecord
		if json.Unmarshal(line, &r) != nil || !reSession.MatchString(r.Session) {
			continue
		}
		if i, ok := idx[r.Session]; ok {
			if r.End != "" {
				out[i].End, out[i].Outcome = r.End, r.Outcome
			}
			continue
		}
		idx[r.Session] = len(out)
		out = append(out, r)
	}
	return out
}

// RoleLine is one agent role of a report: the orchestrator, a reviewer subagent.
type RoleLine struct {
	Role    string  `json:"role"`
	Kind    string  `json:"kind"`
	Model   string  `json:"model"`
	Runs    int     `json:"runs"`
	Tokens  int     `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// Report is what the Reports screen shows of one stored (or freshly built) session report.
type Report struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	First      string     `json:"first"`
	Last       string     `json:"last"`
	Tokens     int        `json:"tokens"`
	TokenState string     `json:"tokenState"`
	CostUSD    float64    `json:"costUsd"`
	CostState  string     `json:"costState"` // complete | partial | unavailable: partial is a lower bound
	Roles      []RoleLine `json:"roles"`
	Warnings   []string   `json:"warnings"`
	Summary    string     `json:"summary"`
	Spec       string     `json:"spec,omitempty"`   // from the run ledger only
	Action     string     `json:"action,omitempty"` //
	Outcome    string     `json:"outcome,omitempty"`
}

func parseReport(b []byte) (Report, bool) {
	var v struct {
		Kind    string `json:"kind"`
		Summary string `json:"summary"`
		Session struct {
			ID    string `json:"id"`
			State string `json:"state"`
			First string `json:"first_record"`
			Last  string `json:"last_record"`
		} `json:"session"`
		Tokens struct {
			Total int    `json:"total"`
			State string `json:"state"`
		} `json:"tokens"`
		Cost struct {
			USD   float64 `json:"api_equivalent_usd"`
			State string  `json:"state"`
		} `json:"cost"`
		Roles []struct {
			RoleLine
			Tokens int `json:"tokens"`
		} `json:"roles"`
		Warnings []string `json:"warnings"`
	}
	if json.Unmarshal(b, &v) != nil || v.Session.ID == "" {
		return Report{}, false
	}
	r := Report{ID: v.Session.ID, Kind: v.Kind, State: v.Session.State, First: v.Session.First, Last: v.Session.Last, Tokens: v.Tokens.Total,
		TokenState: v.Tokens.State, CostUSD: v.Cost.USD, CostState: v.Cost.State, Summary: clip(v.Summary, 400), Warnings: []string{}, Roles: []RoleLine{}}
	for _, w := range v.Warnings {
		r.Warnings = append(r.Warnings, clip(w, 300))
	}
	for _, x := range v.Roles {
		l := x.RoleLine
		l.Tokens = x.Tokens
		r.Roles = append(r.Roles, l)
	}
	return r, true
}

func kitReports(root string) string {
	_, pj, _ := project.Load(root)
	return filepath.Join(pj.Dir(), "reports")
}

// sessionReport is the stored report of a session, else one built from the records now (the kit never stores it while the session
// may still run). ok is false when the kit has nothing on the session.
func sessionReport(root, id string) (Report, bool) {
	if !reSession.MatchString(id) {
		return Report{}, false
	}
	if b, err := os.ReadFile(filepath.Join(kitReports(root), id+".json")); err == nil {
		if r, ok := parseReport(b); ok {
			return r, true
		}
	}
	var out, errb bytes.Buffer
	e := report.New(root)
	e.Out, e.Err = &out, &errb
	if e.Run([]string{"session", id, "--json"}) != 0 {
		return Report{}, false
	}
	return parseReport(out.Bytes())
}

// lowerBound is the kit's own reading of a state: only "partial" (some runs unmeasured or unpriced) and "unavailable" make a figure
// a lower bound; "estimated" and "recorded" are complete in their own way.
func lowerBound(state string) bool { return state == "partial" || state == "unavailable" }

// SpecRollup is the cost of everything the panel ran for one spec.
type SpecRollup struct {
	Spec     string  `json:"spec"`
	Sessions int     `json:"sessions"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"costUsd"`
	Partial  bool    `json:"partial"` // some session was not fully measured or priced: the sums are lower bounds
	Last     string  `json:"last"`
}

// Reports is the Reports screen: the sessions the kit has a report on, newest first, each linked to a spec when the panel ran it,
// and the per-spec totals.
type Reports struct {
	Sessions []Report     `json:"sessions"`
	Specs    []SpecRollup `json:"specs"`
}

const maxLive = 40

// ListReports joins the kit's reports with the panel's run ledger.
func ListReports(root string) Reports {
	runs := map[string]RunRecord{}
	for sid, spec := range taskLinks(root) { // the kit's own record first: a session that wrote to a spec's task ledger belongs to it
		runs[sid] = RunRecord{Session: sid, Spec: spec}
	}
	for _, r := range ReadRuns(root) { // the panel's record adds what the run was for
		runs[r.Session] = r
	}
	byID := map[string]Report{}
	files, _ := filepath.Glob(filepath.Join(kitReports(root), "*.json"))
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			if r, ok := parseReport(b); ok {
				byID[r.ID] = r
			}
		}
	}
	live := 0
	for id := range runs { // a run whose report is not stored yet (the session ended a moment ago, or never signalled its end)
		if _, ok := byID[id]; !ok && live < maxLive {
			if r, ok := sessionReport(root, id); ok {
				byID[id] = r
				live++
			}
		}
	}
	out := Reports{Sessions: []Report{}, Specs: []SpecRollup{}}
	roll := map[string]*SpecRollup{}
	for id, r := range byID {
		if run, ok := runs[id]; ok {
			r.Spec, r.Action, r.Outcome = run.Spec, run.Action, run.Outcome
			s := roll[run.Spec]
			if s == nil {
				s = &SpecRollup{Spec: run.Spec}
				roll[run.Spec] = s
			}
			s.Sessions++
			s.Tokens += r.Tokens
			s.CostUSD += r.CostUSD
			s.Partial = s.Partial || lowerBound(r.CostState) || lowerBound(r.TokenState)
			if r.Last > s.Last {
				s.Last = r.Last
			}
		}
		out.Sessions = append(out.Sessions, r)
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].Last > out.Sessions[j].Last })
	for _, s := range roll {
		s.CostUSD = float64(int(s.CostUSD*10000+0.5)) / 10000
		out.Specs = append(out.Specs, *s)
	}
	sort.Slice(out.Specs, func(i, j int) bool { return out.Specs[i].Last > out.Specs[j].Last })
	return out
}

// taskLinks maps a session to the spec whose task ledger (<project-dir>/work/<spec-id>/usage.jsonl) it wrote usage lines to. That is the
// kit's own record, so it also links sessions started in a terminal with SDKIT_TASK set. Only slugs that are spec ids count: a
// development task is not a spec.
func taskLinks(root string) map[string]string {
	_, pj, _ := project.Load(root)
	out := map[string]string{}
	for _, sp := range ListSpecs(root) {
		b, err := os.ReadFile(filepath.Join(pj.Dir(), "work", sp.ID, "usage.jsonl"))
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(b, []byte{'\n'}) {
			var l struct {
				Session string `json:"session"`
			}
			if json.Unmarshal(line, &l) == nil && reSession.MatchString(l.Session) {
				out[l.Session] = sp.ID
			}
		}
	}
	return out
}

// taskRoles is who spent what on a spec, across all its sessions, from the kit's own task report (`sdkit report task <id>`).
// Empty when nothing was filed under the spec's id.
func taskRoles(root, id string) []RoleLine {
	_, pj, _ := project.Load(root)
	if _, err := os.Stat(filepath.Join(pj.Dir(), "work", id, "usage.jsonl")); err != nil || !state.ValidSlug(id) {
		return []RoleLine{}
	}
	var out, errb bytes.Buffer
	e := report.New(root)
	e.Out, e.Err = &out, &errb
	if e.Run([]string{"task", id, "--json"}) != 0 {
		return []RoleLine{}
	}
	var v struct {
		Roles []struct {
			RoleLine
			Tokens int `json:"tokens"`
		} `json:"roles"`
	}
	roles := []RoleLine{}
	if json.Unmarshal(out.Bytes(), &v) == nil {
		for _, x := range v.Roles {
			l := x.RoleLine
			l.Tokens = x.Tokens
			roles = append(roles, l)
		}
	}
	return roles
}

// SpecReportsView is the "Raporlar" card of a spec page.
type SpecReportsView struct {
	Sessions []Report   `json:"sessions"`
	Roles    []RoleLine `json:"roles"`
}

// SpecReportsOf is the sessions linked to a spec and the role breakdown of its task report.
func SpecReportsOf(root, id string) SpecReportsView {
	return SpecReportsView{Sessions: SpecReports(root, id), Roles: taskRoles(root, id)}
}

// SpecReports are the reports of the runs the panel made for one spec, newest first, for the spec page.
func SpecReports(root, id string) []Report {
	all := ListReports(root)
	out := []Report{}
	for _, r := range all.Sessions {
		if r.Spec == id {
			out = append(out, r)
		}
	}
	return out
}

// ReportMarkdown is the kit's own text of a session report, for the "open report" view.
func ReportMarkdown(root, id string) (string, error) {
	if !reSession.MatchString(id) {
		return "", errors.New("geçersiz oturum")
	}
	if b, err := os.ReadFile(filepath.Join(kitReports(root), id+".md")); err == nil {
		return string(b), nil
	}
	var out, errb bytes.Buffer
	e := report.New(root)
	e.Out, e.Err = &out, &errb
	if e.Run([]string{"session", id, "--markdown"}) != 0 {
		return "", errors.New("bu oturum için rapor yok")
	}
	return strings.TrimSpace(out.String()), nil
}

func nowStamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }
