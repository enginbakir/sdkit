package runrole

import (
	"sync"

	"github.com/wecii/sdkit/internal/route"
)

// routeSeam is the Router seam (P4-08): the cost-aware router of internal/route behind run-role's Decide / Outcome calls.
// It is installed when the package loads; with ROUTING off run-role never calls it.
type routeSeam struct {
	mu   sync.Mutex
	root string
}

func init() { Seams.Router = &routeSeam{} }

// Decide asks route.sh decide's successor with --require-cli --source run-role; the returned code is the one to exit with when the
// decision does not run (2, 3, 7 or 8), 0 when it does.
func (s *routeSeam) Decide(q Query) (Decision, int, error) {
	s.mu.Lock()
	s.root = q.Root
	s.mu.Unlock()
	d, err := route.New(q.Root).Decide(route.Query{Role: q.Role, Host: q.Host, Tier: q.Tier, Model: q.Model, Handoff: q.Handoff, Rerun: q.Rerun,
		IgnoreBudget: q.IgnoreBudget, RequireCLI: true, Reserve: q.Reserve, PID: q.PID, Source: "run-role"})
	if err != nil {
		return Decision{}, 2, err
	}
	out := Decision{Action: d.Action, Reason: d.Reason, ID: d.ID, Adapter: d.Adapter, Model: d.Model, Effort: d.Effort, Tier: d.Tier,
		Notes: d.Notes, Warnings: d.Warnings, TimeoutS: d.TimeoutS, WaitS: d.WaitS}
	if d.Runs() {
		return out, 0, nil
	}
	return out, d.Exit, nil
}

// Outcome closes the decision once; a failure to record it never changes the run's own exit status.
func (s *routeSeam) Outcome(id string, o Outcome) {
	s.mu.Lock()
	root := s.root
	s.mu.Unlock()
	d := o.DurMS
	_, _ = route.New(root).Close(id, route.Outcome{RC: o.RC, PRC: o.PRC, Stderr: o.Stderr, DurMS: &d, In: o.In, Out: o.Out, Tokens: o.Tokens, CostEst: o.CostEst})
}
