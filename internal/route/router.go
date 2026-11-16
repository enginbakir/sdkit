package route

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/store"
	"github.com/wecii/sdkit/internal/usage"
	"github.com/wecii/sdkit/internal/workflow"
)

// Router is the routing of one project: it gathers the facts, calls Decide and keeps the ledgers (routing.jsonl, reservations.jsonl).
type Router struct {
	Root string
	P    project.Project
	Get  func(string) string
	now  func() time.Time
}

// New loads the project at root (sdkit.env, sdkit.local.env, then the environment).
func New(root string) *Router {
	s, p, _ := project.Load(root)
	return &Router{Root: root, P: p, Get: base.Getter(s.Raw), now: time.Now}
}

// Query is what a caller asks: the flags it was given, never a guess. Reserve records the decision (and the spawn and the money):
// without it the decision is a preview that changes nothing.
type Query struct {
	Role, Host, Tier, Model, Handoff, Risk, Rerun string
	IgnoreBudget, RequireCLI, Reserve             bool
	PID                                           int
	Source                                        string
}

// Slug is the task whose ledgers count: the open task, else the newest one, else _misc.
func (r *Router) Slug() string { return orDef(state.ActiveSlug(r.P), "_misc") }

// roleText reads a role's source: the kit's, else the project's.
func (r *Router) roleText(b string) (string, bool) {
	if t, err := project.KitFile("core/roles/" + b + ".md"); err == nil {
		return string(t), true
	}
	t, err := os.ReadFile(filepath.Join(r.P.Dir(), "roles", b+".md"))
	return string(t), err == nil
}

// Decide gathers the facts and decides. With q.Reserve it holds the project and ledger locks, closes interrupted attempts, counts the
// spawn, reserves money under the hard cost policy and records the decision, so two requests cannot start the same run.
func (r *Router) Decide(q Query) (Decision, error) {
	slug := r.Slug()
	led := r.ledger(slug)
	pt, _ := pricing.Load(PricesFile(r.P, r.Get))
	mb, _ := os.ReadFile(filepath.Join(r.P.Dir(), "models.json"))
	in := KitInput(r.Get, mb, pt)
	in.Slug, in.Role, in.Host, in.Tier, in.Model, in.Rerun, in.IgnoreBudget, in.RequireCLI, in.RoleText = slug, q.Role, q.Host, q.Tier, q.Model, q.Rerun, q.IgnoreBudget, q.RequireCLI, r.roleText
	if q.Reserve {
		in.ID = fmt.Sprintf("rd-%s-%d-%d-%d", slug, r.now().Unix(), os.Getpid(), rand.IntN(32768))
		if err := os.MkdirAll(filepath.Join(r.work(), slug), 0o755); err != nil {
			return Decision{}, err
		}
		if r.Get("ROUTE_COST_ENFORCE") == "hard" { // lock order is fixed: the project lock first, then the task's ledger
			u, err := store.Lock(filepath.Join(r.work(), ".project-budget"))
			if err != nil {
				return Decision{}, err
			}
			defer u()
		}
		u, err := store.Lock(led)
		if err != nil {
			return Decision{}, err
		}
		defer u()
	}
	b := baseRole(q.Role)
	var task *sdd.Task
	e := sdd.New(r.Root)
	if raw, err := os.ReadFile(filepath.Join(r.P.Dir(), ".sdd", "state.json")); err == nil {
		if task, err = sdd.ParseTask(raw); err != nil {
			task = nil
		}
	}
	if task != nil {
		in.Risk, in.Profile = task.Risk, task.Profile
		in.Mandatory = slices.Contains(workflow.EvidenceRoles(task.Risk), b) || b == "verifier" && workflow.Verifier(task.Risk, task.Profile) == "independent-verifier"
	}
	if q.Risk != "" {
		in.Risk, in.RiskSource = q.Risk, "flag"
	}
	// the key of this request: role + handoff + the diff, for the roles whose evidence is bound to it
	hsum := "-"
	if hb, err := os.ReadFile(q.Handoff); q.Handoff != "" && err == nil {
		s := sha256.Sum256(hb)
		hsum = hex.EncodeToString(s[:])[:16]
	}
	fp := ""
	if gate(b) && task != nil {
		fp, _ = e.Fingerprint()
		if fp != "" {
			row := sdd.EvidenceTable(task, fp, []string{b})[0]
			ev := task.Evidence[b]
			in.Ev = Evidence{Known: true, State: row.State, Run: row.Run, BlockedHere: ev.Result == "BLOCK" && ev.Fingerprint == fp}
		}
		if len(fp) > 16 {
			fp = fp[:16]
		}
	}
	key := b + "|" + hsum + "|" + orDef(fp, "-")
	in.Chain, in.InFlight = r.chain(slug, key, b, q.Reserve)
	lines := usage.ReadLines(usage.LedgerFile(r.P, slug))
	in.Use = usageFacts(lines, pt, b)
	if n := usage.Spawns(r.P, slug); n != nil {
		in.Spawns = *n
	}
	if r.Get("ROUTE_COST_ENFORCE") == "hard" {
		in.Hard.Spend = map[string]Spend{"task": spend([]string{usage.LedgerFile(r.P, slug)}, pt, ""), "role": spend([]string{usage.LedgerFile(r.P, slug)}, pt, b)}
		all, _ := filepath.Glob(filepath.Join(r.work(), "*", "usage.jsonl"))
		arch, _ := filepath.Glob(filepath.Join(r.work(), "_archive", "*.usage.jsonl"))
		in.Hard.Spend["project"] = spend(append(all, arch...), pt, "")
		var oerr error
		in.Hard.Open, oerr = r.OpenReserved()
		in.Hard.OpenErr = oerr != nil
		in.Hard.Rate = pt.Lookup
	}
	d := Decide(in)
	if !q.Reserve {
		return d, nil
	}
	if d.Runs() {
		_ = state.Update(r.P, slug, func(s *state.State) error { s.Spawns++; return nil })
		if ch := d.Budget.CostHard; ch != nil {
			_ = r.appendJSON(filepath.Join(r.work(), slug, "reservations.jsonl"), struct {
				Type string  `json:"type"`
				ID   string  `json:"id"`
				TS   string  `json:"ts"`
				Role string  `json:"role"`
				USD  float64 `json:"usd"`
				PID  *int    `json:"pid"`
			}{"reserve", d.ID, stamp(r.now()), b, ch.ReservationUSD, pidp(q.PID)})
		}
	}
	// the ledger line: the decision as printed, without decision_id, plus id, v, type, ts, key, source, pid
	jb, _ := d.MarshalJSON()
	idb, _ := enc(d.ID)
	rest := strings.TrimSuffix(string(jb[len(`{"decision_id":`)+len(idb)+1:]), "}")
	tail, _ := enc(struct {
		ID     string `json:"id"`
		V      int    `json:"v"`
		Type   string `json:"type"`
		TS     string `json:"ts"`
		Key    string `json:"key"`
		Source string `json:"source"`
		PID    *int   `json:"pid"`
	}{d.ID, 1, "decision", stamp(r.now()), key, orDef(q.Source, "cli"), pidp(q.PID)})
	_ = store.AppendLine(led, "{"+rest+","+string(tail[1:]))
	sdd.Tel.Emit("workflow.decision", "task", slug, "source", "routing", "outcome", d.Action, "role", q.Role, "host", d.Adapter, "model", d.Model, "tier", d.Tier, "attempt", fmt.Sprint(d.Attempt), "note", d.Reason+" "+d.ID)
	return d, nil
}

func pidp(p int) *int {
	if p <= 0 {
		return nil
	}
	return &p
}

// chain is this request's ledger entries. A decision with no outcome is in flight while its process lives (or, with no pid, until
// ROUTE_INFLIGHT_TTL_S): that is reported in flight. Otherwise it was interrupted: it gets an INTERRUPTED outcome (written when
// reserve is set) and counts as a failed attempt.
func (r *Router) chain(slug, key, role string, reserve bool) (out []Entry, inflight string) {
	ttl := time.Duration(30*60) * time.Second
	if v := r.Get("ROUTE_INFLIGHT_TTL_S"); isInt(v) {
		var n int
		fmt.Sscan(v, &n)
		ttl = time.Duration(n) * time.Second
	}
	done := map[string]bool{}
	for _, e := range r.readEntries(slug) {
		if e.Key != key {
			continue
		}
		out = append(out, e)
		if e.Type == "outcome" {
			done[e.ID] = true
		}
	}
	for _, e := range out {
		if e.Type != "decision" || done[e.ID] || e.Action != Execute && e.Action != Retry && e.Action != Escalate {
			continue
		}
		alive := false
		if e.PID != nil {
			alive = proc.Alive(*e.PID)
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", e.TS); err == nil {
			alive = r.now().Sub(t) < ttl
		}
		if alive {
			pid := "-"
			if e.PID != nil {
				pid = fmt.Sprint(*e.PID)
			}
			return out, fmt.Sprintf("decision %s for the same request is still running (pid %s)", e.ID, pid)
		}
		if reserve {
			_ = r.appendJSON(r.ledger(slug), struct {
				V     int    `json:"v"`
				Type  string `json:"type"`
				TS    string `json:"ts"`
				ID    string `json:"id"`
				Key   string `json:"key"`
				Role  string `json:"role"`
				Class string `json:"class"`
				RC    *int   `json:"rc"`
				Dur   *int   `json:"dur_ms"`
				Tok   *int   `json:"tokens"`
				TQ    string `json:"tokens_quality"`
			}{1, "outcome", stamp(r.now()), e.ID, key, role, "INTERRUPTED", nil, nil, nil, "unavailable"})
		}
		out = append(out, Entry{Type: "outcome", ID: e.ID, Key: key, Class: "INTERRUPTED"})
	}
	return out, ""
}

// Outcome is how a decided run ended.
type Outcome struct {
	RC, PRC         int
	Stderr          string
	DurMS           *int64
	In, Out         int
	Tokens, CostEst string // Tokens: actual | unavailable
}

// Close records the outcome of decision id, releases its reservation (whatever happened, idempotent) and returns the failure class.
func (r *Router) Close(id string, o Outcome) (string, error) {
	slug := r.Slug()
	var dec *Entry
	all := r.readEntries(slug)
	for i, e := range all {
		if e.Type == "decision" && e.ID == id {
			dec = &all[i]
		}
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no routing ledger for %s", slug)
	}
	if dec == nil {
		return "", fmt.Errorf("unknown decision %s", id)
	}
	cls := Classify(o.RC, o.PRC, o.Stderr)
	if cls == "VERDICT_BLOCK" {
		cls = "VERIFICATION_FAILURE"
		if dec.Role == "security-auditor" {
			cls = "SECURITY_BLOCK"
		}
	}
	type tok struct {
		In  int `json:"in"`
		Out int `json:"out"`
	}
	var tk *tok
	if o.Tokens == "actual" {
		tk = &tok{o.In, o.Out}
	}
	tq := "unavailable"
	if tk != nil {
		tq = "actual"
	}
	rec := struct {
		V     int      `json:"v"`
		Type  string   `json:"type"`
		TS    string   `json:"ts"`
		ID    string   `json:"id"`
		Key   string   `json:"key"`
		Role  string   `json:"role"`
		Class string   `json:"class"`
		RC    int      `json:"rc"`
		Dur   *int64   `json:"dur_ms"`
		Tok   *tok     `json:"tokens"`
		TQ    string   `json:"tokens_quality"`
		Est   *float64 `json:"host_cost_estimate_usd,omitempty"`
		Prov  string   `json:"cost_provenance,omitempty"`
	}{V: 1, Type: "outcome", TS: stamp(r.now()), ID: id, Key: dec.Key, Role: dec.Role, Class: cls, RC: o.RC, Dur: o.DurMS, Tok: tk, TQ: tq}
	if numRe.MatchString(o.CostEst) {
		f := pos(o.CostEst)
		rec.Est, rec.Prov = &f, "host client-side estimate (not billed)"
	}
	if u, err := store.Lock(r.ledger(slug)); err == nil {
		_ = r.appendJSON(r.ledger(slug), rec)
		u()
	}
	r.Release(slug, id, cls, "")
	sdd.Tel.Emit("workflow.decision", "task", slug, "source", "routing", "outcome", "outcome:"+cls, "role", dec.Role, "note", fmt.Sprintf("%s rc=%d", id, o.RC))
	return cls, nil
}

// Validate lists the problems of the ROUTE_* settings, models.json and the preferences; nothing when sound.
func (r *Router) Validate() []string {
	in := r.Input()
	errs := in.cfgErrors()
	if in.RegErr != "" {
		return append(errs, in.RegErr)
	}
	errs = append(errs, RegistryErrors(in.Reg, in.Adapters)...)
	if f := r.Get("ROUTE_FIXED_MODEL"); f != "" {
		if _, ok := Available(in.Reg, f); !ok {
			errs = append(errs, "ROUTE_FIXED_MODEL '"+f+"' is not an available registry id")
		}
	}
	for _, role := range []string{"verifier", "reviewer", "security-auditor", "developer", "test-writer", "engineer", "devsecops", "scout", "analyst"} {
		if p := r.Get("ROUTE_PREFER_" + upper(role)); p != "" {
			if _, ok := Available(in.Reg, p); !ok {
				errs = append(errs, "ROUTE_PREFER_"+upper(role)+": '"+p+"' is not an available registry id")
			}
		}
	}
	return errs
}

// Input is the kit and project data of this router (adapters, registry, prices); the request and the task's facts are not filled in.
func (r *Router) Input() Input {
	pt, _ := pricing.Load(PricesFile(r.P, r.Get))
	mb, _ := os.ReadFile(filepath.Join(r.P.Dir(), "models.json"))
	in := KitInput(r.Get, mb, pt)
	in.RoleText = r.roleText
	return in
}

// Registry is the model registry as the router sees it.
func (r *Router) Registry() ([]Model, error) {
	in := r.Input()
	if in.RegErr != "" {
		return nil, fmt.Errorf("%s", in.RegErr)
	}
	return in.Reg, nil
}

// Log is the routing ledger of a task, one line per decision or outcome.
func (r *Router) Log(slug string) []string {
	var out []string
	for _, e := range r.readEntries(slug) {
		if e.Type == "decision" {
			out = append(out, fmt.Sprintf("%s %s %s %s %s/%s tier=%s attempt=%d %s", e.TS, e.ID, e.Role, e.Action, orDef(e.Adapter, "-"), orDef(e.Model, "default"), orDef(e.Tier, "-"), e.Attempt, e.Reason))
			continue
		}
		rc := "null"
		if e.RC != nil {
			rc = fmt.Sprint(*e.RC)
		}
		out = append(out, fmt.Sprintf("%s %s outcome %s rc=%s", e.TS, e.ID, e.Class, rc))
	}
	return out
}
