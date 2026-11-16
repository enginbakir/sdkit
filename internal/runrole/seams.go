package runrole

// The seams below are where 1.x run-role called scripts that belong to later phases. Each is nil until that phase wires it
// (cmd/sdkit sets Seams from its init); nil means the 1.x behaviour with the feature off.

// Query is what run-role asks the router (route.sh decide): the flags the caller gave, never a guess.
type Query struct {
	Role, Handoff, Host, Model, Tier, Rerun string
	IgnoreBudget, Reserve                   bool // Reserve is false in a dry run
	PID                                     int
	Root                                    string
}

// Decision is the router's answer. Action execute, retry and escalate run; anything else starts nothing.
type Decision struct {
	Action, Reason, ID           string
	Adapter, Model, Effort, Tier string
	Notes, Warnings              []string
	TimeoutS, WaitS              int
}

// Outcome closes a decision exactly once with how the run ended.
type Outcome struct {
	RC, PRC         int // exit status; verdict status (0 when none)
	Stderr          string
	DurMS           int64
	CostEst, Tokens string // Tokens: actual | unavailable
	In, Out         int
}

// Router is P4-08 (ROUTING=on). Decide returns the decision and the exit code to use when it does not run (2, 3, 7 or 8).
// With ROUTING=on and a nil Router, run-role refuses with exit 2.
type Router interface {
	Decide(q Query) (Decision, int, error)
	Outcome(id string, o Outcome)
}

// Usage is one execution for the ledger (usage.sh log). TQ is actual or unavailable: unmeasured is never written as 0.
type Usage struct {
	Role, Tier, Host, Model, ID, TQ, Status, CostEst string
	In, Out, CacheRead, CacheWrite                   int
}

// Budget is P4-05: Check counts a spawn for the task and reports a reached limit ("" within the limits), Log stores usage.
// A nil Budget never limits and logs nothing.
type Budget interface {
	Check(slug string) string
	Log(u Usage)
}

// Contract is P3-05 (internal/contract). Issue validates and registers a handoff (checkOnly: validate only); Result checks the
// answer's HANDOFF-RESULT line and returns 0 valid and done, 1 valid but partial|blocked|failed, else broken; Close
// closes an interrupted one. A nil Contract does not enforce contracts and run-role says so.
type Contract interface {
	Issue(handoff, role string, checkOnly bool) (msg string, ok bool)
	Result(answer, id, role, cwd string) (msg string, code int)
	Close(id, reason string)
}

// Deps are all the seams.
type Deps struct {
	Router   Router
	Budget   Budget
	Contract Contract
	// Envelope turns a long answer into the compact ENVELOPE/1 view (digest, P4); nil prints the answer.
	Envelope func(answer, role, ref string) string
}

// Seams is what Run uses.
var Seams Deps
