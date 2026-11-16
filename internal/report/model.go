// Package report is the Value Report: what a task or a session accomplished, who did it, what it used and cost, and what SDKit
// demonstrably saved. It only READS what the kit already records (the usage ledger, the telemetry events, the routing, context and
// selection records), keeps no second ledger, calls no model, and never changes a decision. N/A is never a guess: a figure is
// labelled actual, estimated or unavailable, and orchestration adds agent runs, so nothing here assumes it saves money.
package report

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// kv and om build a JSON object whose keys keep the order they are written in (the report is read by people and diffed).
type kv struct {
	K string
	V any
}
type om []kv

func (o om) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.K)
		b.Write(k)
		b.WriteByte(':')
		v, err := marshal(e.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal is json.Marshal without HTML escaping and without the trailing newline.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Section types. A pointer is a figure that can be absent (null); nothing is turned into 0.

// RoleRow is one role on one model: runs, tokens and cost. A figure is nil when no run of the row was measured or priced.
type RoleRow struct {
	Role          string   `json:"role"`
	Kind          string   `json:"kind"`
	Model         *string  `json:"model"`
	Provider      *string  `json:"provider"`
	Runs          int      `json:"runs"`
	UnmeasuredRun int      `json:"unmeasured_runs"`
	In            *int     `json:"in"`
	Out           *int     `json:"out"`
	CacheRead     *int     `json:"cache_read"`
	CacheWrite    *int     `json:"cache_write"`
	Tokens        *int     `json:"tokens"`
	CostUSD       *float64 `json:"cost_usd"`
	CostComplete  bool     `json:"cost_complete"`
	CostReason    string   `json:"cost_reason"`
}

// TaskRole is a RoleRow with what a task report adds: the purpose of the role, why it ran, and its result.
type TaskRole struct {
	RoleRow
	Purpose    *string  `json:"purpose"`
	Why        []string `json:"why"`
	ResultText string   `json:"result_text"`
}

// FilesChanged is the number of changed files and how it was counted.
type FilesChanged struct {
	N      int    `json:"n"`
	Method string `json:"method"`
}

// Tests is the test outcome of the task; State "not recorded" has no counts.
type Tests struct {
	State       string `json:"state"`
	Passed      *int   `json:"passed,omitempty"`
	Failed      *int   `json:"failed,omitempty"`
	RedExpected *int   `json:"red_expected,omitempty"`
}

// Gate is the quality gate: Pending, Not run, Passed or Failed.
type Gate struct {
	State      string  `json:"state"`
	Note       *string `json:"note,omitempty"`
	Final      *string `json:"final,omitempty"`
	Rejections *int    `json:"rejections,omitempty"`
}

// Duration is the wall time and where it came from.
type Duration struct {
	S      *float64 `json:"s"`
	Source string   `json:"source"`
}

// TaskInfo is section "task" of a task report.
type TaskInfo struct {
	Name            string        `json:"name"`
	ID              string        `json:"id"`
	Run             string        `json:"run"`
	Status          string        `json:"status"`
	StatusRequested string        `json:"status_requested"`
	Terminal        bool          `json:"terminal"`
	StatusSource    string        `json:"status_source"`
	Duration        Duration      `json:"duration"`
	Profile, Risk   *string       `json:"-"`
	Hosts           []string      `json:"hosts"`
	PrimaryModel    *string       `json:"primary_model"`
	Sessions        []string      `json:"sessions"`
	FilesChanged    *FilesChanged `json:"files_changed"`
	Tests           Tests         `json:"tests"`
	QualityGates    Gate          `json:"quality_gates"`
}

// MarshalJSON writes the workflow object (profile, risk) where the bash report had it.
func (t TaskInfo) MarshalJSON() ([]byte, error) {
	return om{{"name", t.Name}, {"id", t.ID}, {"run", t.Run}, {"status", t.Status}, {"status_requested", t.StatusRequested}, {"terminal", t.Terminal},
		{"status_source", t.StatusSource}, {"duration", t.Duration}, {"workflow", om{{"profile", t.Profile}, {"risk", t.Risk}}}, {"hosts", t.Hosts},
		{"primary_model", t.PrimaryModel}, {"sessions", t.Sessions}, {"files_changed", t.FilesChanged}, {"tests", t.Tests}, {"quality_gates", t.QualityGates}}.MarshalJSON()
}

// UnmarshalJSON reads what MarshalJSON wrote.
func (t *TaskInfo) UnmarshalJSON(b []byte) error {
	type plain TaskInfo
	var w struct {
		plain
		Workflow struct{ Profile, Risk *string }
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*t = TaskInfo(w.plain)
	t.Profile, t.Risk = w.Workflow.Profile, w.Workflow.Risk
	return nil
}

// Executions counts distinct executions.
type Executions struct {
	Total      int            `json:"total"`
	ByKind     map[string]int `json:"by_kind"`
	Unmeasured int            `json:"unmeasured"`
}

// Tokens is the token section; State unavailable (no ledger row) carries only the note.
type Tokens struct {
	State          string   `json:"state"`
	In             *int     `json:"in,omitempty"`
	Out            *int     `json:"out,omitempty"`
	CacheRead      *int     `json:"cache_read,omitempty"`
	CacheWrite     *int     `json:"cache_write,omitempty"`
	Total          *int     `json:"total,omitempty"`
	Fresh          *int     `json:"fresh,omitempty"`
	MeasuredRuns   *int     `json:"measured_runs,omitempty"`
	UnmeasuredRuns *int     `json:"unmeasured_runs,omitempty"`
	CacheHitRate   *float64 `json:"cache_hit_rate,omitempty"`
	Note           string   `json:"note,omitempty"`
}

// MarshalJSON keeps null where the bash report had null (cache_hit_rate) and leaves out what an unavailable section never had.
func (t Tokens) MarshalJSON() ([]byte, error) {
	if t.State == "unavailable" {
		return om{{"state", t.State}, {"note", t.Note}}.MarshalJSON()
	}
	return om{{"state", t.State}, {"in", t.In}, {"out", t.Out}, {"cache_read", t.CacheRead}, {"cache_write", t.CacheWrite}, {"total", t.Total}, {"fresh", t.Fresh},
		{"measured_runs", t.MeasuredRuns}, {"unmeasured_runs", t.UnmeasuredRuns}, {"cache_hit_rate", t.CacheHitRate}, {"note", t.Note}}.MarshalJSON()
}

// Cost is the cost section: API-equivalent, an estimate from the price table, never a bill.
type Cost struct {
	State          string   `json:"state"`
	APIEquivalent  *float64 `json:"api_equivalent_usd"`
	PricedRuns     int      `json:"priced_runs"`
	UnpricedRuns   int      `json:"unpriced_runs"`
	PricingSource  string   `json:"pricing_source"`
	Billed         string   `json:"billed"`
	HostEstimate   *float64 `json:"host_estimate_usd"`
	CacheSaving    *float64 `json:"cache_saving_usd"`
	CacheSavingTxt string   `json:"cache_saving_note"`
	Reasons        []string `json:"reasons"`
	short          bool     // no ledger row at all: the section is only its state
}

// MarshalJSON: an unavailable cost is just its state.
func (c Cost) MarshalJSON() ([]byte, error) {
	if c.short {
		return om{{"state", c.State}}.MarshalJSON()
	}
	return om{{"state", c.State}, {"api_equivalent_usd", c.APIEquivalent}, {"priced_runs", c.PricedRuns}, {"unpriced_runs", c.UnpricedRuns}, {"pricing_source", c.PricingSource},
		{"billed", c.Billed}, {"host_estimate_usd", c.HostEstimate}, {"cache_saving_usd", c.CacheSaving}, {"cache_saving_note", c.CacheSavingTxt}, {"reasons", c.Reasons}}.MarshalJSON()
}

// Routing is the counterfactual: the same measured tokens of every non-orchestrator run priced on the baseline model.
type Routing struct {
	State         string   `json:"state"`
	Reason        string   `json:"reason"`
	BaselineModel string   `json:"baseline_model"`
	Runs          int      `json:"runs"`
	PricedRuns    int      `json:"priced_runs"`
	OnBaseline    int      `json:"on_baseline"`
	Cheaper       int      `json:"cheaper"`
	Dearer        int      `json:"dearer"`
	ActualUSD     float64  `json:"actual_usd"`
	BaselineUSD   float64  `json:"baseline_usd"`
	DeltaUSD      float64  `json:"delta_usd"`
	Models        []string `json:"models"`
}

// MarshalJSON: "na" has only a reason.
func (r Routing) MarshalJSON() ([]byte, error) {
	if r.State != "ok" {
		return om{{"state", r.State}, {"reason", r.Reason}}.MarshalJSON()
	}
	return om{{"state", r.State}, {"baseline_model", r.BaselineModel}, {"runs", r.Runs}, {"priced_runs", r.PricedRuns}, {"on_baseline", r.OnBaseline}, {"cheaper", r.Cheaper},
		{"dearer", r.Dearer}, {"actual_usd", r.ActualUSD}, {"baseline_usd", r.BaselineUSD}, {"delta_usd", r.DeltaUSD}, {"models", r.Models}}.MarshalJSON()
}

// Item is one avoided execution: counted always, quantified only where a comparable recorded run of the role exists.
type Item struct {
	ID         string   `json:"-"`
	Role       string   `json:"role"`
	Cls        string   `json:"cls"`
	Source     *string  `json:"source"`
	Confidence *string  `json:"confidence"`
	Quantified bool     `json:"quantified"`
	In         *float64 `json:"-"`
	Out        *float64 `json:"-"`
	CacheRead  *float64 `json:"-"`
	CacheWrite *float64 `json:"-"`
	CostUSD    *float64 `json:"-"`
}

// Avoided is the avoided executions seen in the routing decisions.
type Avoided struct {
	Count      int            `json:"count"`
	ByClass    map[string]int `json:"by_class"`
	Quantified int            `json:"quantified"`
	Items      []Item         `json:"items"`
}

// Component is one part of the cost saving.
type Component struct {
	Name       string  `json:"name"`
	USD        float64 `json:"usd"`
	Source     string  `json:"source"`
	Confidence string  `json:"confidence"`
}

// SavTokens is the token saving: "na" with a reason, or an estimate.
type SavTokens struct {
	State          string   `json:"state"`
	Text           string   `json:"text,omitempty"`
	Why            string   `json:"why,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	BaselineTokens float64  `json:"baseline_tokens"`
	ActualTokens   float64  `json:"actual_tokens"`
	SavedTokens    float64  `json:"saved_tokens"`
	RatePct        *float64 `json:"rate_pct"`
	BaselineSource string   `json:"baseline_source"`
	Method         string   `json:"method"`
	Accounting     string   `json:"accounting"`
	Coverage       string   `json:"coverage"`
	Confidence     string   `json:"confidence"`
}

// MarshalJSON writes the variant of the state.
func (s SavTokens) MarshalJSON() ([]byte, error) {
	if s.State == "na" {
		return om{{"state", s.State}, {"text", s.Text}, {"why", s.Why}}.MarshalJSON()
	}
	return om{{"state", s.State}, {"kind", s.Kind}, {"baseline_tokens", s.BaselineTokens}, {"actual_tokens", s.ActualTokens}, {"saved_tokens", s.SavedTokens}, {"rate_pct", s.RatePct},
		{"baseline_source", s.BaselineSource}, {"method", s.Method}, {"accounting", s.Accounting}, {"coverage", s.Coverage}, {"confidence", s.Confidence}}.MarshalJSON()
}

// SavCost is the cost saving: "na" with a reason, or an estimate.
type SavCost struct {
	State       string      `json:"state"`
	Text        string      `json:"text,omitempty"`
	Why         string      `json:"why,omitempty"`
	Kind        string      `json:"kind,omitempty"`
	BaselineUSD float64     `json:"baseline_usd"`
	ActualUSD   float64     `json:"actual_usd"`
	SavedUSD    float64     `json:"saved_usd"`
	RatePct     *float64    `json:"rate_pct"`
	Components  []Component `json:"components"`
	Method      string      `json:"method"`
	Coverage    string      `json:"coverage"`
	Confidence  string      `json:"confidence"`
	Regression  bool        `json:"regression"`
}

// MarshalJSON writes the variant of the state.
func (s SavCost) MarshalJSON() ([]byte, error) {
	if s.State == "na" {
		return om{{"state", s.State}, {"text", s.Text}, {"why", s.Why}}.MarshalJSON()
	}
	return om{{"state", s.State}, {"kind", s.Kind}, {"baseline_usd", s.BaselineUSD}, {"actual_usd", s.ActualUSD}, {"saved_usd", s.SavedUSD}, {"rate_pct", s.RatePct},
		{"components", s.Components}, {"method", s.Method}, {"coverage", s.Coverage}, {"confidence", s.Confidence}, {"regression", s.Regression}}.MarshalJSON()
}

// Savings is the savings section of a task report.
type Savings struct {
	Tokens  SavTokens `json:"tokens"`
	Cost    SavCost   `json:"cost"`
	Routing Routing   `json:"routing"`
	Avoided Avoided   `json:"avoided"`
}

// Optimization is one observed optimization; Class says how it is known (estimated, measured count, measured, heuristic, qualitative).
type Optimization struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	Evidence string `json:"evidence"`
	Benefit  string `json:"benefit"`
}

// Validation lists the tests and verification results the report rests on.
type Validation struct {
	Tests        []string       `json:"tests"`
	Verification []string       `json:"verification"`
	Final        *string        `json:"final"`
	Retries      *int           `json:"retries"`
	Failures     map[string]int `json:"failures"`
}

// TaskReport is a task's report (schema value-report/1).
type TaskReport struct {
	Schema        string         `json:"schema"`
	Kind          string         `json:"kind"`
	Key           string         `json:"key"`
	GeneratedAt   string         `json:"generated_at"`
	Regenerated   bool           `json:"regenerated"`
	Task          TaskInfo       `json:"task"`
	Roles         []TaskRole     `json:"roles"`
	Executions    Executions     `json:"executions"`
	Tokens        Tokens         `json:"tokens"`
	Cost          Cost           `json:"cost"`
	Savings       Savings        `json:"savings"`
	Optimizations []Optimization `json:"optimizations"`
	Validation    Validation     `json:"validation"`
	Warnings      []string       `json:"warnings"`
	Summary       string         `json:"summary"`
}

// SessionInfo is section "session" of a session report.
type SessionInfo struct {
	ID          string  `json:"id"`
	State       string  `json:"state"`
	EndReason   string  `json:"end_reason"`
	EndedAt     *string `json:"ended_at"`
	FirstRecord *string `json:"first_record"`
	LastRecord  *string `json:"last_record"`
	Note        string  `json:"note"`
}

// SessionTask is one stored task report of the session.
type SessionTask struct {
	Task        string   `json:"task"`
	Status      string   `json:"status"`
	TokensFresh *int     `json:"tokens_fresh"`
	CostUSD     *float64 `json:"cost_usd"`
}

// SessionTasks counts the task reports of the session.
type SessionTasks struct {
	Total     int           `json:"total"`
	Completed int           `json:"completed"`
	Partial   int           `json:"partial"`
	Failed    int           `json:"failed"`
	Blocked   int           `json:"blocked"`
	List      []SessionTask `json:"list"`
}

// RoleRuns is how often a role ran in a session.
type RoleRuns struct {
	Role  string   `json:"role"`
	Runs  int      `json:"runs"`
	Kinds []string `json:"kinds"`
}

// SessionExec counts a session's executions.
type SessionExec struct {
	Total      int        `json:"total"`
	ByRole     []RoleRuns `json:"by_role"`
	Unmeasured int        `json:"unmeasured"`
}

// SessionAvoided is the avoided executions of the tasks fully inside the session.
type SessionAvoided struct {
	Counted  map[string]int `json:"counted_in_session_tasks"`
	TokensEs *float64       `json:"tokens_saved_estimate"`
	USDEs    *float64       `json:"usd_saved_estimate"`
	Excluded string         `json:"excluded"`
}

// SessionSavTokens is the session token saving.
type SessionSavTokens struct {
	State string  `json:"state"`
	Saved float64 `json:"saved_tokens"`
	Note  string  `json:"note"`
	Text  string  `json:"text"`
}

// MarshalJSON writes the variant of the state.
func (s SessionSavTokens) MarshalJSON() ([]byte, error) {
	if s.State == "na" {
		return om{{"state", s.State}, {"text", s.Text}}.MarshalJSON()
	}
	return om{{"state", s.State}, {"saved_tokens", s.Saved}, {"note", s.Note}}.MarshalJSON()
}

// SessionSavCost is the session cost saving.
type SessionSavCost struct {
	State   string   `json:"state"`
	Saved   float64  `json:"saved_usd"`
	Routing *float64 `json:"routing"`
	Avoided *float64 `json:"avoided_executions"`
	Text    string   `json:"text"`
	Why     string   `json:"why"`
}

// MarshalJSON writes the variant of the state.
func (s SessionSavCost) MarshalJSON() ([]byte, error) {
	if s.State == "na" {
		return om{{"state", s.State}, {"text", s.Text}, {"why", s.Why}}.MarshalJSON()
	}
	return om{{"state", s.State}, {"saved_usd", s.Saved}, {"components", om{{"routing", s.Routing}, {"avoided_executions", s.Avoided}}}}.MarshalJSON()
}

// SessionSavings is the savings of a session.
type SessionSavings struct {
	Routing    Routing          `json:"routing"`
	RoutingUSD *float64         `json:"routing_usd"`
	Avoided    SessionAvoided   `json:"avoided_executions"`
	Tokens     SessionSavTokens `json:"tokens"`
	Cost       SessionSavCost   `json:"cost"`
}

// SessionValidation sums the tests and gates of the session's task reports (nil with no task report: not 0).
type SessionValidation struct {
	TestsPassed *int           `json:"tests_passed"`
	TestsFailed *int           `json:"tests_failed"`
	Gates       map[string]int `json:"gates"`
}

// SessionReport is a session's report (schema value-report/1).
type SessionReport struct {
	Schema        string            `json:"schema"`
	Kind          string            `json:"kind"`
	Key           string            `json:"key"`
	GeneratedAt   string            `json:"generated_at"`
	Regenerated   bool              `json:"regenerated"`
	Session       SessionInfo       `json:"session"`
	Tasks         SessionTasks      `json:"tasks"`
	Executions    SessionExec       `json:"executions"`
	Roles         []RoleRow         `json:"roles"`
	Tokens        Tokens            `json:"tokens"`
	Cost          Cost              `json:"cost"`
	Savings       SessionSavings    `json:"savings"`
	EvidenceReuse int               `json:"evidence_reuse"`
	Skipped       int               `json:"skipped"`
	Validation    SessionValidation `json:"validation"`
	Optimizations []string          `json:"optimizations_observed"`
	Warnings      []string          `json:"warnings"`
	Summary       string            `json:"summary"`
}

// MarshalJSON of a session cost: the session report keeps a shorter cost section than a task report.
func sessionCost(c Cost) om {
	if c.short {
		return om{{"state", c.State}}
	}
	return om{{"state", c.State}, {"api_equivalent_usd", c.APIEquivalent}, {"priced_runs", c.PricedRuns}, {"unpriced_runs", c.UnpricedRuns}, {"pricing_source", c.PricingSource},
		{"billed", "unavailable"}, {"cache_saving_usd", c.CacheSaving}}
}

// MarshalJSON writes the shorter session cost and the shorter session tokens.
func (s SessionReport) MarshalJSON() ([]byte, error) {
	t := om{{"state", s.Tokens.State}}
	if s.Tokens.State != "unavailable" {
		t = om{{"state", s.Tokens.State}, {"in", s.Tokens.In}, {"out", s.Tokens.Out}, {"cache_read", s.Tokens.CacheRead}, {"cache_write", s.Tokens.CacheWrite},
			{"total", s.Tokens.Total}, {"fresh", s.Tokens.Fresh}, {"cache_hit_rate", s.Tokens.CacheHitRate}}
	}
	return om{{"schema", s.Schema}, {"kind", s.Kind}, {"key", s.Key}, {"generated_at", s.GeneratedAt}, {"regenerated", s.Regenerated}, {"session", s.Session}, {"tasks", s.Tasks},
		{"executions", s.Executions}, {"roles", s.Roles}, {"tokens", t}, {"cost", sessionCost(s.Cost)}, {"savings", s.Savings}, {"evidence_reuse", s.EvidenceReuse},
		{"skipped", s.Skipped}, {"validation", s.Validation}, {"optimizations_observed", s.Optimizations}, {"warnings", s.Warnings}, {"summary", s.Summary}}.MarshalJSON()
}

// Index is one line of work/_reports/index.jsonl: enough of a stored report to list it, compare it and feed a session summary.
type Index struct {
	Key         string         `json:"key"`
	Kind        string         `json:"kind"`
	ID          string         `json:"id"`
	Task        string         `json:"task,omitempty"`
	Status      string         `json:"status"`
	TS          string         `json:"ts"`
	File        string         `json:"file"`
	Sessions    []string       `json:"sessions,omitempty"`
	Tokens      *int           `json:"tokens_fresh,omitempty"`
	Cost        *float64       `json:"cost_usd,omitempty"`
	SavedUSD    *float64       `json:"saved_usd,omitempty"`
	SavedTokens *float64       `json:"saved_tokens,omitempty"`
	ByClass     map[string]int `json:"avoided_by_class,omitempty"`
	SavedAvoid  float64        `json:"saved_usd_avoided,omitempty"`
	TestsPassed int            `json:"tests_passed,omitempty"`
	TestsFailed int            `json:"tests_failed,omitempty"`
	Gates       string         `json:"gates,omitempty"`
	Optims      []string       `json:"optimizations,omitempty"`
	Tasks       *int           `json:"tasks,omitempty"`
}

// MarshalJSON keeps null figures of a task line (the bash index line had them) and the fixed keys of each kind.
func (x Index) MarshalJSON() ([]byte, error) {
	if x.Kind != "task" {
		return om{{"key", x.Key}, {"kind", x.Kind}, {"id", x.ID}, {"status", x.Status}, {"ts", x.TS}, {"file", x.File}, {"tasks", x.Tasks}}.MarshalJSON()
	}
	return om{{"key", x.Key}, {"kind", x.Kind}, {"id", x.ID}, {"task", x.Task}, {"status", x.Status}, {"ts", x.TS}, {"file", x.File}, {"sessions", x.Sessions},
		{"tokens_fresh", x.Tokens}, {"cost_usd", x.Cost}, {"saved_usd", x.SavedUSD}, {"saved_tokens", x.SavedTokens}, {"avoided_by_class", orMap(x.ByClass)},
		{"saved_usd_avoided", x.SavedAvoid}, {"tests_passed", x.TestsPassed}, {"tests_failed", x.TestsFailed}, {"gates", x.Gates}, {"optimizations", x.Optims}}.MarshalJSON()
}

func orMap(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

// Number helpers: jq rounds half away from zero, and prints a number in its shortest form.

func r4(x float64) float64 { return math.Round(x*10000) / 10000 }
func r1(x float64) float64 { return math.Round(x*10) / 10 }
func ptr[T any](v T) *T    { return &v }
func rp4(x *float64) *float64 {
	if x == nil {
		return nil
	}
	return ptr(r4(*x))
}

// num is a number the way jq prints it in a string: 6.6, 51.9, 100.
func num(x float64) string {
	if a := math.Abs(x); a != 0 && (a < 1e-5 || a >= 1e17) {
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return strconv.FormatFloat(x, 'f', -1, 64)
}
