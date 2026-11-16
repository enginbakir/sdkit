package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Summary is `telemetry.sh summary --json` for one run, as far as the Value Report reads it: phases, retries, failures, tests, verification,
// contracts and the workflow decisions. The agent, token, cost and ledger cross-check sections of the bash summary are not ported: nothing
// reads them (the report takes tokens and cost from the usage ledger).
type Summary struct {
	Task         string       `json:"task"`
	Run          string       `json:"run"`
	Mode         string       `json:"mode"`
	Risk         *string      `json:"risk"`
	Status       string       `json:"status"`
	Verified     bool         `json:"verified"`
	Started      string       `json:"started"`
	Ended        *string      `json:"ended"`
	WallS        float64      `json:"wall_s"`
	Phases       []PhaseSpan  `json:"phases"`
	Retries      int          `json:"retries"`
	Failures     Failures     `json:"failures"`
	Tests        []TestCount  `json:"tests"`
	Verification Verification `json:"verification"`
	Contracts    Contracts    `json:"contracts"`
	Workflow     Workflow     `json:"workflow"`
	Events       int          `json:"events"`
}

// PhaseSpan is how long the task stayed in a phase (DurS nil for the last open one).
type PhaseSpan struct {
	Phase string   `json:"phase"`
	At    string   `json:"at"`
	DurS  *float64 `json:"dur_s"`
}

// Failures counts failure events by category and refused phase moves.
type Failures struct {
	Total          int            `json:"total"`
	ByCategory     map[string]int `json:"by_category"`
	GateRejections int            `json:"gate_rejections"`
}

// TestCount is the test.result events of one phase and outcome.
type TestCount struct {
	Phase   string `json:"phase"`
	Outcome string `json:"outcome"`
	Runs    int    `json:"runs"`
}

// VerifyResult is one verify.result event.
type VerifyResult struct {
	Outcome string  `json:"outcome"`
	Source  *string `json:"source"`
	Role    *string `json:"role"`
}

// Verification lists the verify.result events and the last outcome (nil when none).
type Verification struct {
	Results []VerifyResult `json:"results"`
	Final   *string        `json:"final"`
}

// Contracts counts contract.check events.
type Contracts struct {
	Checked      int            `json:"checked"`
	Valid        int            `json:"valid"`
	Invalid      int            `json:"invalid"`
	Handoffs     int            `json:"handoffs"`
	Results      int            `json:"results"`
	ByReason     map[string]int `json:"by_reason"`
	ByStatus     map[string]int `json:"by_status"`
	HandoffBytes int            `json:"handoff_bytes"`
	ResultBytes  int            `json:"result_bytes"`
}

// Decision is one workflow.decision event.
type Decision struct {
	Source  string  `json:"source"`
	Outcome string  `json:"outcome"`
	Reason  *string `json:"reason"`
}

// Workflow is the profile and the workflow decisions of the run.
type Workflow struct {
	Profile            *string        `json:"profile"`
	Decisions          []Decision     `json:"decisions"`
	RejectionsByReason map[string]int `json:"rejections_by_reason"`
}

func sv(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case nil:
		return "null"
	default:
		return fmt.Sprint(v)
	}
}

func sp(m map[string]any, k string) *string {
	if s, ok := m[k].(string); ok {
		return &s
	}
	return nil
}

func secs(a, b string) (float64, error) {
	ta, e1 := time.Parse("2006-01-02T15:04:05Z", a)
	tb, e2 := time.Parse("2006-01-02T15:04:05Z", b)
	if e1 != nil || e2 != nil {
		return 0, errors.New("bad timestamp")
	}
	return tb.Sub(ta).Seconds(), nil
}

func count(es []map[string]any, f func(map[string]any) bool) (n int) {
	for _, e := range es {
		if f(e) {
			n++
		}
	}
	return n
}

func tally(es []map[string]any, key func(map[string]any) string) map[string]int {
	m := map[string]int{}
	for _, e := range es {
		m[key(e)]++
	}
	return m
}

// Summarize reads an events file and summarizes one run ("" = the run of the last task.start, else of the last event). It fails when the
// file has no event of that run, or an event it needs has a timestamp that is not ISO 8601 UTC.
func Summarize(file, run string) (*Summary, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var all []map[string]any
	for _, l := range strings.Split(string(b), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m != nil && m["ts"] != nil && m["type"] != nil {
			all = append(all, m)
		}
	}
	if run == "" && len(all) > 0 {
		last := all[len(all)-1]
		for _, e := range all {
			if sv(e, "type") == "task.start" {
				last = e
			}
		}
		run, _ = last["run"].(string)
	}
	var e []map[string]any
	for _, m := range all {
		if r, _ := m["run"].(string); r == run {
			e = append(e, m)
		}
	}
	if len(e) == 0 {
		return nil, errors.New("no events for this run")
	}
	of := func(typ string) (out []map[string]any) {
		for _, m := range e {
			if sv(m, "type") == typ {
				out = append(out, m)
			}
		}
		return out
	}
	var ts, te map[string]any
	if x := of("task.start"); len(x) > 0 {
		ts = x[0]
	}
	if x := of("task.end"); len(x) > 0 {
		te = x[len(x)-1]
	}
	ph := of("phase.enter")
	first := e[0]
	if ts != nil {
		first = ts
	}
	endts := sv(e[len(e)-1], "ts")
	if te != nil {
		endts = sv(te, "ts")
	}
	s := &Summary{Task: sv(e[0], "task"), Run: run, Status: "in-progress", Started: sv(first, "ts"), Events: len(e)}
	modes := map[string]bool{}
	for _, m := range e {
		mode, _ := m["mode"].(string)
		if mode == "" {
			mode = "live"
		}
		modes[mode] = true
	}
	for m := range modes {
		s.Mode = m
	}
	if len(modes) != 1 {
		s.Mode = "mixed"
	}
	if ts != nil {
		s.Risk = sp(ts, "risk")
		s.Workflow.Profile = sp(ts, "profile")
	}
	if te != nil {
		s.Status, s.Ended = sv(te, "outcome"), sp(te, "ts")
	}
	for _, m := range ph {
		if sv(m, "to") == "verified" {
			s.Verified = true
		}
	}
	if s.WallS, err = secs(s.Started, endts); err != nil {
		return nil, err
	}
	type mark struct{ phase, at string }
	var marks []mark
	if ts != nil {
		p, _ := ts["phase"].(string)
		if p == "" {
			p = "intake"
		}
		marks = append(marks, mark{p, sv(ts, "ts")})
	}
	for _, m := range ph {
		marks = append(marks, mark{sv(m, "to"), sv(m, "ts")})
	}
	for i, m := range marks {
		ps := PhaseSpan{Phase: m.phase, At: m.at}
		var d float64
		switch {
		case i+1 < len(marks):
			d, err = secs(m.at, marks[i+1].at)
			ps.DurS = &d
		case te != nil && m.phase != "done":
			d, err = secs(m.at, sv(te, "ts"))
			ps.DurS = &d
		}
		if err != nil {
			return nil, err
		}
		s.Phases = append(s.Phases, ps)
	}
	s.Retries = len(of("retry"))
	fails := of("failure")
	s.Failures = Failures{Total: len(fails), ByCategory: tally(fails, func(m map[string]any) string { return sv(m, "category") }), GateRejections: len(of("phase.reject"))}
	tests := tally(of("test.result"), func(m map[string]any) string {
		p := "-"
		if v, ok := m["phase"].(string); ok {
			p = v
		}
		return p + "\x00" + sv(m, "outcome")
	})
	for k, n := range tests {
		pr := strings.SplitN(k, "\x00", 2)
		s.Tests = append(s.Tests, TestCount{pr[0], pr[1], n})
	}
	sort.Slice(s.Tests, func(i, j int) bool {
		a, b := s.Tests[i], s.Tests[j]
		return a.Phase < b.Phase || (a.Phase == b.Phase && a.Outcome < b.Outcome)
	})
	for _, m := range of("verify.result") {
		s.Verification.Results = append(s.Verification.Results, VerifyResult{sv(m, "outcome"), sp(m, "source"), sp(m, "role")})
		s.Verification.Final = sp(m, "outcome")
	}
	cc := of("contract.check")
	num := func(m map[string]any) int { f, _ := m["bytes"].(float64); return int(f) }
	s.Contracts = Contracts{Checked: len(cc), ByReason: map[string]int{}, ByStatus: map[string]int{}}
	for _, m := range cc {
		valid := sv(m, "outcome") == "valid"
		switch {
		case valid:
			s.Contracts.Valid++
		case sv(m, "outcome") == "invalid":
			s.Contracts.Invalid++
			s.Contracts.ByReason[sv(m, "reason")]++
		}
		switch sv(m, "stage") {
		case "handoff":
			s.Contracts.Handoffs++
			s.Contracts.HandoffBytes += num(m)
		case "result":
			s.Contracts.Results++
			s.Contracts.ResultBytes += num(m)
			if valid {
				s.Contracts.ByStatus[sv(m, "status")]++
			}
		}
	}
	for _, m := range of("workflow.decision") {
		s.Workflow.Decisions = append(s.Workflow.Decisions, Decision{sv(m, "source"), sv(m, "outcome"), sp(m, "reason")})
	}
	s.Workflow.RejectionsByReason = tally(of("phase.reject"), func(m map[string]any) string {
		if r, ok := m["reason"].(string); ok {
			return r
		}
		return "unspecified"
	})
	return s, nil
}
