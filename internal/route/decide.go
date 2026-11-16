package route

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/wecii/sdkit/internal/pricing"
)

// Entry is one line of the routing ledger (a decision or its outcome) of the request being decided.
type Entry struct {
	Type     string `json:"type"`
	Key      string `json:"key"`
	Role     string `json:"role"`
	ID       string `json:"id"`
	Action   string `json:"action"`
	Adapter  string `json:"adapter"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	Tier     string `json:"tier"`
	Enforced bool   `json:"tier_enforced"`
	Class    string `json:"class"`
	TS       string `json:"ts"`
	Attempt  int    `json:"attempt"`
	Reason   string `json:"reason"`
	RC       *int   `json:"rc"`
	PID      *int   `json:"pid"`
}

// Evidence is the independent role's evidence for this diff, as internal/sdd judges it. Known is false for work roles and when
// there is no open task or no fingerprint; State is valid, missing, block, stale, untrusted, wrong-task or conflict.
type Evidence struct {
	Known, BlockedHere bool
	State, Run         string
}

// Usage is what the task's usage ledger says so far; Est is nil while nothing is priced.
type Usage struct {
	N, Unavailable, Tokens, RoleTokens int
	Est, HostEst                       *float64
	HostRuns, Unpriced                 int
}

// Spend is one scope of the hard cost policy: priced USD and the runs that are unmeasured or unpriced.
type Spend struct {
	USD     float64
	Unknown int
}

// Hard are the facts of ROUTE_COST_ENFORCE=hard: the spend of each scope, the open reservations (OpenErr when unreadable) and the price table.
type Hard struct {
	Spend   map[string]Spend // task, role, project
	Open    float64
	OpenErr bool
	Rate    func(model string) (pricing.Rate, bool)
}

// Input is everything a decision depends on. Decide reads nothing else (no file, no clock, no process).
type Input struct {
	Get                            func(string) string // a setting ("" when unset)
	Slug, ID                       string
	Role, Host, Tier, Model, Rerun string
	IgnoreBudget, RequireCLI       bool
	Risk, RiskSource, Profile      string // the task's, or the --risk flag (RiskSource flag)
	Mandatory                      bool
	RoleText                       func(base string) (string, bool)
	Adapters                       *Adapters
	AdaptersErr                    bool
	Reg                            []Model
	RegErr                         string
	Ev                             Evidence
	Chain                          []Entry // this request's decisions and outcomes; an interrupted decision already has its INTERRUPTED outcome
	InFlight                       string  // "decision X ... (pid P)" when a run of the same request is still alive
	Use                            Usage
	Spawns                         int
	Hard                           Hard
	HasCLI                         func(cli string) bool
	HasTimeout                     bool
}

type cfg struct {
	retries, escs, attempts, backoff int
	escTo, fixed, policy             string
	floor                            map[string]string
	fallback                         []string
	down                             bool
}

func (in Input) get(k, def string) string { return orDef(in.Get(k), def) }

func (in Input) num(k string, def int) int {
	if v, err := strconv.Atoi(in.get(k, "")); err == nil {
		return v
	}
	return def
}

// cfgErrors is one line per problem in the settings; nothing when sound.
func (in Input) cfgErrors() (errs []string) {
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	g := in.Get
	isnum := func(s string) bool { return numRe.MatchString(s) }
	switch r := in.get("ROUTING", "off"); r {
	case "on", "off":
	default:
		add("ROUTING must be on or off (is '%s')", r)
	}
	switch p := in.get("ROUTE_POLICY", "tiered"); p {
	case "tiered":
	case "fixed":
		if g("ROUTE_FIXED_MODEL") == "" {
			add("ROUTE_POLICY=fixed needs ROUTE_FIXED_MODEL=<registry id>")
		}
	default:
		add("ROUTE_POLICY must be tiered or fixed (is '%s')", p)
	}
	for _, v := range []string{"ROUTE_MAX_RETRIES", "ROUTE_MAX_ESCALATIONS", "ROUTE_MAX_ATTEMPTS", "ROUTE_BACKOFF_S", "ROUTE_INFLIGHT_TTL_S"} {
		if x := g(v); x != "" && !isInt(x) {
			add("%s must be a non-negative integer", v)
		}
	}
	for _, v := range []string{"ROUTE_TIMEOUT_S", "ROUTE_ROLE_TOKENS"} {
		if x := g(v); x != "" && !isInt(x) {
			add("%s must be a non-negative integer", v)
		}
	}
	if x := g("ROUTE_BUDGET_COST"); x != "" && !isnum(x) {
		add("ROUTE_BUDGET_COST must be a number (USD), like 5 or 0.25")
	}
	enforce := in.get("ROUTE_COST_ENFORCE", "off")
	if enforce != "off" && enforce != "hard" {
		add("ROUTE_COST_ENFORCE must be off or hard")
	}
	for _, v := range []string{"ROUTE_ROLE_BUDGET_COST", "ROUTE_PROJECT_BUDGET_COST", "ROUTE_RESERVE_USD"} {
		if x := g(v); x != "" && !isnum(x) {
			add("%s must be a number (USD), like 5 or 0.25", v)
		}
	}
	if x := g("ROUTE_RESERVE_USD"); x != "" && isnum(x) && enforce == "hard" && pos(x) <= 0 {
		add("ROUTE_RESERVE_USD must be greater than 0 under ROUTE_COST_ENFORCE=hard (a zero reservation reserves nothing)")
	}
	if x := g("ROUTE_RESERVE_TTL_S"); x != "" && !isInt(x) {
		add("ROUTE_RESERVE_TTL_S must be a non-negative integer (seconds)")
	}
	if enforce == "hard" && g("ROUTE_BUDGET_COST") == "" && g("ROUTE_ROLE_BUDGET_COST") == "" && g("ROUTE_PROJECT_BUDGET_COST") == "" {
		add("ROUTE_COST_ENFORCE=hard needs ROUTE_BUDGET_COST, ROUTE_ROLE_BUDGET_COST or ROUTE_PROJECT_BUDGET_COST")
	}
	if d := in.get("ROUTE_DOWNGRADE_LOW", "0"); d != "0" && d != "1" {
		add("ROUTE_DOWNGRADE_LOW must be 0 or 1")
	}
	for _, t := range []string{in.get("ROUTE_ESCALATE_TO", "strong"), in.get("ROUTE_FLOOR_LOW", "light"), in.get("ROUTE_FLOOR_MEDIUM", "light"), in.get("ROUTE_FLOOR_HIGH", "standard")} {
		if Rank(t) == 0 {
			add("'%s' is not a tier (light|standard|strong)", t)
		}
	}
	for _, h := range strings.Fields(g("ROUTE_FALLBACK_HOSTS")) {
		if _, ok := in.Adapters.M[h]; in.Adapters != nil && !ok {
			add("ROUTE_FALLBACK_HOSTS: unknown host '%s'", h)
		}
	}
	if in.Adapters == nil || in.AdaptersErr {
		add("adapters.json is missing or not valid JSON")
	}
	return errs
}

func pos(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func (in Input) config() cfg {
	return cfg{retries: in.num("ROUTE_MAX_RETRIES", 2), escs: in.num("ROUTE_MAX_ESCALATIONS", 1), attempts: in.num("ROUTE_MAX_ATTEMPTS", 4), backoff: in.num("ROUTE_BACKOFF_S", 10),
		escTo: in.get("ROUTE_ESCALATE_TO", "strong"), fixed: in.Get("ROUTE_FIXED_MODEL"), policy: in.get("ROUTE_POLICY", "tiered"), fallback: strings.Fields(in.Get("ROUTE_FALLBACK_HOSTS")),
		floor: map[string]string{"low": in.get("ROUTE_FLOOR_LOW", "light"), "medium": in.get("ROUTE_FLOOR_MEDIUM", "light"), "high": in.get("ROUTE_FLOOR_HIGH", "standard")}, down: in.Get("ROUTE_DOWNGRADE_LOW") == "1"}
}

// Frontmatter reads one key of a role file's frontmatter.
func Frontmatter(text, key string) string {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimRight(l, "\r") == "---" {
			if n++; n == 2 {
				return ""
			}
			continue
		}
		if n == 1 && strings.HasPrefix(l, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(l, key+":"))
		}
	}
	return ""
}

func upper(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) }

func fnum(f float64) string { b, _ := json.Marshal(f); return string(b) }

// Decide is the router: a pure function of its Input. It never lowers a risk floor, skips a mandatory gate or turns a BLOCK into a PASS.
func Decide(in Input) (d Decision) {
	c := in.config()
	g := in.Get
	d = Decision{ID: in.ID, Task: in.Slug, Role: in.Role, RoleClass: "work", Action: Execute, Reason: "ROLE_CAPABILITY_MATCH", RiskSource: orDef(in.RiskSource, "task"),
		Profile: orDef(in.Profile, "standard"), TierEnforced: true, Attempt: 1, Routing: in.get("ROUTING", "off"), Mandatory: in.Mandatory}
	if d.ID == "" {
		d.ID = "rd-" + in.Slug + "-preview"
	}
	note := func(s string) { d.Notes = append(d.Notes, s) }
	warn := func(s string) { d.Warnings = append(d.Warnings, s) }
	stop := func(reason string, notes ...string) Decision {
		d.Action, d.Reason = Stop, reason
		d.Notes = append(d.Notes, notes...)
		switch reason {
		case "BUDGET_EXHAUSTED":
			d.Exit = ExitBudget
		case "CONFIG_INVALID", "UNKNOWN_ROLE", "NO_ADAPTER", "ADAPTER_UNSUPPORTED", "NO_COMPATIBLE_MODEL", "OVERRIDE_INVALID", "OVERRIDE_BELOW_FLOOR", "PREFERENCE_INVALID", "PREFERENCE_BELOW_FLOOR", "FIXED_INVALID", "FIXED_BELOW_FLOOR", "NO_ESCALATION_TARGET":
			d.Exit = ExitRefused
		default:
			d.Exit = ExitStop
		}
		return d
	}
	if e := in.cfgErrors(); len(e) > 0 {
		return stop("CONFIG_INVALID", strings.Join(e[:min(3, len(e))], ";"))
	}
	if in.RegErr != "" {
		return stop("CONFIG_INVALID", in.RegErr)
	}
	if e := RegistryErrors(in.Reg, in.Adapters); len(e) > 0 {
		return stop("CONFIG_INVALID", strings.Join(e[:min(3, len(e))], ";"))
	}
	// role
	base := strings.TrimSuffix(in.Role, "-deep")
	text, ok := in.RoleText(base)
	if !ok {
		return stop("UNKNOWN_ROLE", "no role text for '"+base+"'")
	}
	rtier := orDef(Frontmatter(text, "tier"), "standard")
	deep := Frontmatter(text, "deep")
	if in.Role != base && deep != "" {
		rtier = deep
	} else if in.Role == base { // AGENT_<ROLE>_TIER; an independent role is never moved below its own tier
		if v := g("AGENT_" + upper(base) + "_TIER"); Rank(v) > 0 {
			if gate(base) && Rank(v) < Rank(rtier) {
				v = rtier
			}
			rtier = v
		}
	}
	switch {
	case gate(base):
		d.RoleClass = "gate"
	case base == "scout" || base == "analyst":
		d.RoleClass = "lookup"
	}
	// task facts
	d.Risk = in.Risk
	switch in.Risk {
	case "low", "medium", "high":
	case "":
		d.RiskSource = "assumed"
		note("no open task: risk assumed medium, no downgrade")
	default:
		return stop("CONFIG_INVALID", "risk must be low|medium|high")
	}
	effRisk := orDef(in.Risk, "medium")
	// evidence: internal/sdd is the one judge of validity; a BLOCK that stands for this diff is never re-judged
	if d.RoleClass == "gate" && in.Ev.Known {
		if in.Ev.BlockedHere {
			if base == "security-auditor" {
				return stop("SECURITY_BLOCK", "the "+base+" BLOCKed this diff; a BLOCK is not re-run on the same diff (another model or provider cannot overturn it): change the diff first")
			}
			return stop("VERIFICATION_FAILED", "the "+base+" BLOCKed this diff; fix it (state.sh retry) so the diff changes, then run the "+base+" again")
		}
		switch in.Ev.State {
		case "valid":
			if in.Rerun == "" {
				d.Action, d.Reason, d.Exit = Reuse, "EVIDENCE_REUSED", ExitReuse
				note("valid trusted " + base + " PASS for this diff (run " + orDef(in.Ev.Run, "-") + ")")
				return d
			}
		case "missing", "":
		default:
			note("recorded " + base + " evidence is not valid for this diff (" + in.Ev.State + "): running it again")
		}
	}
	// chain: previous attempts of this exact request
	if in.InFlight != "" {
		return stop("DUPLICATE_IN_FLIGHT", in.InFlight)
	}
	var last *Entry
	aN, aRetry, aEsc, sameClass := 0, 0, 0, 0
	var lClass string
	var failed []string
	for i, e := range in.Chain {
		if e.Type != "decision" || !slices.Contains([]string{Execute, Retry, Escalate}, e.Action) {
			continue
		}
		aN++
		aRetry += b2i(e.Action == Retry)
		aEsc += b2i(e.Action == Escalate)
		last = &in.Chain[i]
	}
	if last != nil {
		for _, e := range in.Chain {
			if e.Type == "outcome" && e.ID == last.ID {
				lClass = e.Class
			}
		}
		for _, e := range in.Chain {
			if e.Type == "outcome" && e.Class == lClass {
				sameClass++
			}
			if e.Type == "outcome" && e.Class == "HOST_MISSING" {
				for _, x := range in.Chain {
					if x.Type == "decision" && x.ID == e.ID && !slices.Contains(failed, x.Adapter) {
						failed = append(failed, x.Adapter)
					}
				}
			}
		}
		slices.Sort(failed)
	}
	d.Attempt = aN + 1
	mode := "fresh"
	if lClass != "" && in.Rerun == "" {
		d.Previous = &Prev{last.ID, lClass}
		if aN >= c.attempts {
			return stop("RETRIES_EXHAUSTED", fmt.Sprintf("attempt limit ROUTE_MAX_ATTEMPTS=%d reached (last failure %s)", c.attempts, lClass))
		}
		switch lClass {
		case "OK":
			if d.RoleClass != "gate" {
				return stop("DUPLICATE_COMPLETED", "this exact request already completed; pass --rerun <reason> to run it again")
			}
			note("a previous run exited ok but left no valid evidence")
		case "TRANSIENT", "RATE_LIMIT", "TIMEOUT", "INTERRUPTED":
			if aRetry >= c.retries {
				return stop("RETRIES_EXHAUSTED", fmt.Sprintf("infrastructure retry limit ROUTE_MAX_RETRIES=%d reached after %s", c.retries, lClass))
			}
			mode, d.Reason = "retry", "RETRY_"+lClass
			if lClass == "RATE_LIMIT" {
				d.WaitS = c.backoff * (aRetry + 1)
			}
		case "INVALID_OUTPUT", "CONTRACT_FAILURE":
			if sameClass <= 1 && aRetry < c.retries {
				mode, d.Reason = "retry", "RETRY_"+lClass
			} else if aEsc < c.escs {
				mode, d.Reason = "escalate", "ESCALATE_"+lClass
			} else {
				return stop("ESCALATION_LIMIT", fmt.Sprintf("%s repeated and ROUTE_MAX_ESCALATIONS=%d is used", lClass, c.escs))
			}
		case "HOST_MISSING":
			mode = "fallback"
		case "SECURITY_BLOCK":
			return stop("SECURITY_BLOCK", "a BLOCK is final for this request; changing provider does not change it")
		case "VERIFICATION_FAILURE":
			return stop("VERIFICATION_FAILED", "the role reported BLOCK; fix the change (state.sh retry), then ask again with a new request")
		case "REPORTED_FAILURE":
			return stop("REPORTED_FAILURE", "the role reported partial|blocked|failed: that is a code-correction decision for the workflow, not an infrastructure retry")
		case "BUDGET_EXHAUSTED", "NOT_RUN":
		default:
			return stop("UNCLASSIFIED_FAILURE", "failure class "+lClass+" has no automatic retry; read the host's error and decide")
		}
	} else if lClass != "" {
		d.Reason = "RERUN_REQUESTED"
		note("rerun: " + clip(strings.ReplaceAll(in.Rerun, "\n", " "), 80))
	}
	switch mode {
	case "retry", "fallback":
		d.Action = Retry
	case "escalate":
		d.Action = Escalate
	}
	// tier: the role's own, the size override, then the risk floors
	needed, whyTier := rtier, ""
	if want := in.Tier; want != "" {
		if m, ok := map[string]string{"small": "light", "medium": "standard", "large": "strong"}[want]; ok {
			want = m
		}
		if Rank(want) == 0 {
			return stop("CONFIG_INVALID", "--tier must be light|standard|strong (or small|medium|large)")
		}
		if d.RoleClass != "gate" || Rank(want) > Rank(needed) {
			needed = want
		}
	} else if c.down && d.RoleClass == "work" && in.Risk == "low" && rtier == "standard" {
		needed, whyTier = "light", "DOWNGRADE_LOW_RISK"
	}
	if d.RoleClass != "lookup" {
		fl := c.floor[effRisk]
		if base == "verifier" && in.Risk == "high" && Rank(deep) > Rank(fl) {
			fl = deep
		}
		if Rank(fl) > Rank(needed) {
			needed, whyTier = fl, "RISK_FLOOR_APPLIED"
		}
	}
	var lTier, lAdapter, lModel, lEffort string
	lEnforced := true
	if last != nil {
		lTier, lAdapter, lModel, lEffort, lEnforced = last.Tier, last.Adapter, last.Model, last.Effort, last.Enforced
	}
	if mode == "escalate" {
		if !lEnforced {
			return stop("NO_ESCALATION_TARGET", "the previous attempt ran on the host default model, whose tier cannot be raised")
		}
		nr := Rank(orDef(lTier, needed)) + 1
		if nr > Rank(c.escTo) {
			return stop("NO_ESCALATION_TARGET", "already at the escalation ceiling ROUTE_ESCALATE_TO="+c.escTo)
		}
		needed, whyTier = tierOf(nr), "ESCALATION"
	}
	// adapter candidates
	primary := in.Host
	if primary == "" && d.RoleClass == "gate" {
		primary = g("VERIFIER_HOST")
	}
	if primary == "" {
		primary = g("RUN_ROLE_HOST")
	}
	var prefe, fixede *Model
	pref := g("ROUTE_PREFER_" + upper(base))
	if pref != "" && in.Model == "" {
		m, ok := Available(in.Reg, pref)
		if !ok {
			return stop("PREFERENCE_INVALID", "ROUTE_PREFER_"+upper(base)+"='"+pref+"' is not an available registry id")
		}
		prefe = &m
	}
	if c.policy == "fixed" {
		m, ok := Available(in.Reg, c.fixed)
		if !ok {
			return stop("FIXED_INVALID", "ROUTE_FIXED_MODEL='"+c.fixed+"' is not an available registry id")
		}
		fixede = &m
	}
	var cands []string
	switch {
	case in.Host != "":
		cands = []string{in.Host}
	case mode != "fresh" && lAdapter != "" && mode != "fallback":
		cands = []string{lAdapter}
	case fixede != nil:
		cands = []string{fixede.Adapter}
	case prefe != nil:
		cands = []string{prefe.Adapter}
	default:
		cands = append([]string{primary}, c.fallback...)
	}
	var uniq []string
	for _, a := range cands {
		if a != "" && !slices.Contains(uniq, a) {
			uniq = append(uniq, a)
		}
	}
	if len(uniq) == 0 {
		return stop("NO_ADAPTER", "no host: pass --host or set RUN_ROLE_HOST (VERIFIER_HOST for independent roles) in .sdkit/sdkit.env")
	}
	if fixede != nil && in.Host != "" && in.Host != fixede.Adapter {
		return stop("OVERRIDE_INVALID", fmt.Sprintf("--host %s conflicts with ROUTE_FIXED_MODEL (%s runs on %s)", in.Host, c.fixed, fixede.Adapter))
	}
	var pick *Model
	first, lastWhy, lastNote := "", "NO_COMPATIBLE_MODEL", ""
	for _, a := range uniq {
		if first == "" {
			first = a
		}
		if slices.Contains(failed, a) {
			continue
		}
		ad, known := in.Adapters.M[a]
		if !known {
			lastWhy, lastNote = "ADAPTER_UNSUPPORTED", "unknown adapter '"+a+"'"
			continue
		}
		if ad.Caps["noninteractive"] == "none" || ad.Caps["tool_use"] == "none" {
			lastWhy, lastNote = "ADAPTER_UNSUPPORTED", "adapter "+a+" cannot run noninteractively with tools"
			continue
		}
		if in.RequireCLI && !in.HasCLI(ad.CLI) {
			lastWhy, lastNote = "ADAPTER_UNSUPPORTED", "CLI '"+ad.CLI+"' (adapter "+a+") is not installed"
			failed = append(failed, a)
			continue
		}
		var elig []Model
		for _, m := range in.Reg {
			if m.Adapter == a && m.Status == "available" && m.handles(base) && (m.Tier == "*" || Rank(m.Tier) >= Rank(needed)) {
				elig = append(elig, m)
			}
		}
		slices.SortStableFunc(elig, func(x, y Model) int { return tierKey(x) - tierKey(y) })
		inElig := func(id string) bool { return slices.ContainsFunc(elig, func(m Model) bool { return m.ID == id }) }
		switch {
		case in.Model != "":
			var ent *Model
			for i, m := range in.Reg {
				if m.Adapter == a && (m.model() == in.Model || m.ID == in.Model) {
					ent = &in.Reg[i]
					break
				}
			}
			if ent == nil {
				lastWhy, lastNote = "OVERRIDE_INVALID", "model '"+in.Model+"' is not in the registry for adapter "+a+" (add it to .sdkit/models.json)"
				continue
			}
			if ent.Status != "available" {
				lastWhy, lastNote = "OVERRIDE_INVALID", "model '"+in.Model+"' is disabled"
				continue
			}
			if !inElig(ent.ID) {
				lastWhy, lastNote = "OVERRIDE_BELOW_FLOOR", fmt.Sprintf("model '%s' (%s) is below the required tier %s or not allowed for %s", in.Model, ent.Tier, needed, base)
				continue
			}
			pick, d.Reason = ent, "OVERRIDE_APPLIED"
		case fixede != nil:
			if !inElig(fixede.ID) {
				lastWhy, lastNote = "FIXED_BELOW_FLOOR", "ROUTE_FIXED_MODEL "+c.fixed+" is below the required tier "+needed+" or not allowed for "+base+": a fixed policy never lowers a floor"
				continue
			}
			pick, d.Reason = fixede, "FIXED_POLICY"
		case prefe != nil:
			if !inElig(prefe.ID) {
				lastWhy, lastNote = "PREFERENCE_BELOW_FLOOR", "ROUTE_PREFER_"+upper(base)+"="+pref+" is below the required tier "+needed+" or not allowed for "+base
				continue
			}
			pick, d.Reason = prefe, "PREFERENCE_APPLIED"
		case mode == "retry" && lAdapter+lModel != "":
			for i, m := range in.Reg {
				if m.Adapter == lAdapter && m.model() == lModel && m.effort() == lEffort {
					pick = &in.Reg[i]
					break
				}
			}
		}
		if pick == nil && len(elig) > 0 {
			pick = &elig[0]
		}
		if pick != nil {
			break
		}
		lastWhy, lastNote = "NO_COMPATIBLE_MODEL", fmt.Sprintf("no available %s model for role %s at tier %s or above (sdkit route registry)", a, base, needed)
	}
	if pick == nil {
		if mode == "escalate" {
			return stop("NO_ESCALATION_TARGET", lastNote)
		}
		return stop(lastWhy, lastNote)
	}
	d.Adapter, d.Model, d.Effort, d.Tier = pick.Adapter, pick.model(), pick.effort(), needed
	pad := in.Adapters.M[pick.Adapter]
	if pick.Tier == "*" {
		d.TierEnforced = false
		warn("MODEL_INHERITED: adapter " + d.Adapter + " has no configured model for tier " + needed + ", so the host's default model runs; its capability tier is not verified")
	}
	if d.Model != "" && pad.Caps["model_selection"] == "none" {
		return stop("ADAPTER_UNSUPPORTED", "adapter "+d.Adapter+" cannot select a model, so "+d.Model+" cannot be honoured")
	}
	if d.Reason == "ROLE_CAPABILITY_MATCH" && whyTier != "" {
		d.Reason = whyTier
	}
	switch {
	case mode == "escalate":
		d.Reason = "ESCALATE_" + lClass
	case mode == "fallback":
		d.Reason = "ADAPTER_FALLBACK"
		note("adapter(s) failed with HOST_MISSING: " + strings.Join(failed, " "))
	case mode == "fresh" && d.Adapter != first && in.Host == "":
		d.Reason = "ADAPTER_FALLBACK"
		note(first + " could not run this role: " + lastNote)
	}
	return in.budget(&d, base, aN, pad, c, stop)
}

func gate(base string) bool {
	return base == "verifier" || base == "reviewer" || base == "security-auditor"
}

func tierKey(m Model) int {
	if m.Tier == "*" {
		return 99
	}
	return Rank(m.Tier)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
