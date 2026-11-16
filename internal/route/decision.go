package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Actions of a decision. execute, retry and escalate run the role; reuse (exit 7) and stop (exit 8, 3 or 2) start nothing.
const (
	Execute  = "execute"
	Retry    = "retry"
	Escalate = "escalate"
	Reuse    = "reuse"
	Stop     = "stop"
)

// Exit codes of a decision that does not run.
const (
	ExitRefused = 2 // refused by policy or configuration (the reason says which setting)
	ExitBudget  = 3 // budget exhausted
	ExitReuse   = 7 // valid evidence for this diff already exists: success-like, nothing to run
	ExitStop    = 8 // nothing run and nothing valid exists: never success
)

// Decision is the machine-readable answer; Exit is the status a caller returns when Action is not a run (0 when it is).
type Decision struct {
	ID, Task, Role, RoleClass string
	Mandatory                 bool
	Risk, RiskSource, Profile string
	Action                    string
	Adapter, Model, Effort    string
	Tier                      string
	TierEnforced              bool
	TimeoutS                  int
	Budget                    Budget
	Attempt, WaitS            int
	Reason, Routing           string
	Previous                  *Prev
	Notes, Warnings           []string
	Exit                      int
}

// Prev is the failed attempt this one follows.
type Prev struct {
	DecisionID   string `json:"decision_id"`
	FailureClass string `json:"failure_class"`
}

// Runs reports whether the role is to be executed.
func (d Decision) Runs() bool {
	return d.Action == Execute || d.Action == Retry || d.Action == Escalate
}

// Budget is the record of every limit the decision was measured against. A limit not configured is absent.
type Budget struct {
	Invocations    *Inv      `json:"invocations,omitempty"`
	TaskTokens     *TaskTok  `json:"task_tokens,omitempty"`
	RoleTokens     *Inv      `json:"role_tokens,omitempty"`
	Attempts       *Att      `json:"attempts,omitempty"`
	TimeoutS       *int      `json:"timeout_s,omitempty"`
	TimeoutEnforce string    `json:"timeout_enforcement,omitempty"`
	Cost           *Cost     `json:"cost,omitempty"`
	CostHard       *CostHard `json:"cost_hard,omitempty"`
	Accounting     string    `json:"accounting,omitempty"`
}

type Inv struct {
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Remaining   int    `json:"remaining"`
	Enforcement string `json:"enforcement"`
}

type TaskTok struct {
	Limit       int    `json:"limit"`
	Used        *int   `json:"used"`
	Remaining   *int   `json:"remaining"`
	Enforcement string `json:"enforcement"`
	Scope       string `json:"scope"`
}

type Att struct {
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Enforcement string `json:"enforcement"`
}

type Cost struct {
	LimitUSD         float64  `json:"limit_usd"`
	EstimatedUSD     *float64 `json:"estimated_usd"`
	ActualUSD        *float64 `json:"actual_usd"`
	EstimateComplete bool     `json:"estimate_complete"`
	HostEstimateUSD  *float64 `json:"host_reported_estimate_usd"`
	HostRuns         int      `json:"host_reported_runs"`
	HostProvenance   string   `json:"host_reported_provenance"`
	Enforcement      string   `json:"enforcement"`
}

type CostHard struct {
	Task           *Scope  `json:"task,omitempty"`
	Role           *Scope  `json:"role,omitempty"`
	Project        *Scope  `json:"project,omitempty"`
	Enforcement    string  `json:"enforcement"`
	ReservationUSD float64 `json:"reservation_usd"`
	Basis          string  `json:"reservation_basis"`
	Estimate       string  `json:"basis"`
}

type Scope struct {
	LimitUSD     float64 `json:"limit_usd"`
	EstimatedUSD float64 `json:"estimated_usd"`
	UnknownRuns  int     `json:"unknown_runs"`
	OpenUSD      float64 `json:"open_reservations_usd"`
	ThisUSD      float64 `json:"this_reservation_usd"`
	RemainingUSD float64 `json:"remaining_usd"`
}

// MarshalJSON writes the one-line decision with the keys of 1.x in their order (null for what is not set).
func (d Decision) MarshalJSON() ([]byte, error) {
	var tmo *int
	if d.TimeoutS != 0 {
		tmo = &d.TimeoutS
	}
	w := struct {
		ID           string   `json:"decision_id"`
		Task         string   `json:"task"`
		Role         string   `json:"role"`
		RoleClass    string   `json:"role_class"`
		Mandatory    bool     `json:"mandatory"`
		Risk         *string  `json:"risk"`
		RiskSource   string   `json:"risk_source"`
		Profile      string   `json:"profile"`
		Action       string   `json:"action"`
		Adapter      *string  `json:"adapter"`
		Model        *string  `json:"model"`
		Effort       *string  `json:"effort"`
		Tier         *string  `json:"tier"`
		TierEnforced bool     `json:"tier_enforced"`
		TimeoutS     *int     `json:"timeout_s"`
		Budget       Budget   `json:"budget"`
		Attempt      int      `json:"attempt"`
		WaitS        int      `json:"wait_s"`
		Reason       string   `json:"reason"`
		Policy       string   `json:"policy_version"`
		Routing      string   `json:"routing"`
		Previous     *Prev    `json:"previous,omitempty"`
		Notes        []string `json:"notes,omitempty"`
		Warnings     []string `json:"warnings,omitempty"`
	}{d.ID, d.Task, d.Role, d.RoleClass, d.Mandatory, sp(d.Risk), d.RiskSource, d.Profile, d.Action, sp(d.Adapter), sp(d.Model), sp(d.Effort), sp(d.Tier),
		d.TierEnforced, tmo, d.Budget, d.Attempt, d.WaitS, d.Reason, Policy, d.Routing, d.Previous, d.Notes, d.Warnings}
	return enc(w)
}

// enc is compact JSON without HTML escaping (what jq prints).
func enc(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	err := e.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}

// Explain is the decision as readable lines (never recorded).
func (d Decision) Explain() string {
	bud := []string{}
	for _, x := range []struct {
		n string
		v string
	}{{"invocations", enf(d.Budget.Invocations)}, {"task_tokens", enfT(d.Budget.TaskTokens)}, {"role_tokens", enf(d.Budget.RoleTokens)}, {"attempts", enfA(d.Budget.Attempts)}} {
		if x.v != "" {
			bud = append(bud, x.n+"="+x.v)
		}
	}
	if c := d.Budget.Cost; c != nil {
		bud = append(bud, "cost="+c.Enforcement)
	}
	if c := d.Budget.CostHard; c != nil {
		bud = append(bud, "cost_hard="+c.Enforcement)
	}
	if d.Budget.TimeoutS != nil {
		bud = append(bud, "timeout="+d.Budget.TimeoutEnforce)
	}
	b := "none configured"
	if len(bud) > 0 {
		b = strings.Join(bud, " ")
	}
	mand, enfd, eff, risk, model := "", "", "", "unknown", "host default"
	if d.Mandatory {
		mand = ", mandatory"
	}
	if !d.TierEnforced {
		enfd = ", NOT enforced: the host default model is used"
	}
	if d.Effort != "" {
		eff = ", effort " + d.Effort
	}
	if d.Risk != "" {
		risk = d.Risk
	}
	if d.Model != "" {
		model = d.Model
	}
	s := fmt.Sprintf("Task: %s\nRisk: %s (profile %s)\nRole: %s (%s%s)\nDecision: %s\nAdapter: %s\nModel: %s (tier %s%s%s)\nReason: %s\nBudget: %s\nPolicy: %s  Routing: %s",
		d.Task, risk, d.Profile, d.Role, d.RoleClass, mand, d.Action, orDef(d.Adapter, "-"), model, orDef(d.Tier, "-"), enfd, eff, d.Reason, b, Policy, d.Routing)
	if d.Routing == "off" {
		s += " (run-role ignores this decision until ROUTING=on)"
	}
	if len(d.Warnings) > 0 {
		s += "\nWarnings: " + strings.Join(d.Warnings, "; ")
	}
	if len(d.Notes) > 0 {
		s += "\nNotes: " + strings.Join(d.Notes, "; ")
	}
	return s
}

func enf(i *Inv) string {
	if i == nil {
		return ""
	}
	return i.Enforcement
}

func enfT(i *TaskTok) string {
	if i == nil {
		return ""
	}
	return i.Enforcement
}

func enfA(i *Att) string {
	if i == nil {
		return ""
	}
	return i.Enforcement
}
