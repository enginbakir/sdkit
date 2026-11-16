package route

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wecii/sdkit/internal/pricing"
)

// budget measures the decision against every limit: invocations, tokens, attempts, timeout, advisory cost and (opt-in) the hard cost
// policy. A limit reached before the spawn stops it (exit 3) unless SDD_MODE says otherwise; unknown never passes a hard limit.
func (in Input) budget(d *Decision, base string, attempts int, pad Adapter, c cfg, stop func(string, ...string) Decision) Decision {
	g := in.Get
	warn := func(s string) { d.Warnings = append(d.Warnings, s) }
	note := func(s string) { d.Notes = append(d.Notes, s) }
	mode := in.get("SDD_MODE", "strict")
	u := in.Use
	acct := "complete"
	if u.Unavailable > 0 {
		acct = "partial"
	}
	if u.N > 0 && u.Unavailable == u.N {
		acct = "unavailable"
	}
	adv := pad.Caps["usage_reporting"] == "none"
	tokEnf := "hard"
	if acct != "complete" || adv {
		tokEnf = "advisory"
	}
	if adv && (g("TOKEN_BUDGET_TASK") != "" || g("ROUTE_ROLE_TOKENS") != "") {
		warn("USAGE_UNAVAILABLE: adapter " + d.Adapter + " reports no token usage, so token budgets are advisory (runs on it are not counted)")
	}
	if acct == "partial" {
		warn(fmt.Sprintf("USAGE_PARTIAL: %d of %d earlier run(s) reported no token usage; the used figure is a lower bound", u.Unavailable, u.N))
	}
	var over []string
	b := &d.Budget
	limit := func(k string) int { v, _ := strconv.Atoi(g(k)); return v }
	if l := limit("MAX_SPAWNS"); isInt(g("MAX_SPAWNS")) && l != 0 {
		b.Invocations = &Inv{l, in.Spawns, max(l-in.Spawns, 0), "hard"}
		if in.Spawns+1 > l {
			over = append(over, fmt.Sprintf("invocation limit MAX_SPAWNS=%d reached (%d used)", l, in.Spawns))
		}
	}
	warnPct := false
	if l := limit("TOKEN_BUDGET_TASK"); isInt(g("TOKEN_BUDGET_TASK")) && l != 0 {
		t := &TaskTok{Limit: l, Enforcement: tokEnf, Scope: "no new run starts at or over the limit; a running process is not cut off"}
		if acct != "unavailable" {
			rem := max(l-u.Tokens, 0)
			t.Used, t.Remaining = &u.Tokens, &rem
			if u.Tokens >= l {
				over = append(over, fmt.Sprintf("task token budget TOKEN_BUDGET_TASK=%d reached (%d used)", l, u.Tokens))
			}
			wp := 70
			if isInt(g("TOKEN_BUDGET_WARN_PCT")) {
				wp = limit("TOKEN_BUDGET_WARN_PCT")
			}
			warnPct = u.Tokens*100/l >= wp
		}
		b.TaskTokens = t
	}
	if l := limit("ROUTE_ROLE_TOKENS"); isInt(g("ROUTE_ROLE_TOKENS")) && l != 0 {
		b.RoleTokens = &Inv{l, u.RoleTokens, max(l-u.RoleTokens, 0), tokEnf}
		if u.RoleTokens >= l {
			over = append(over, fmt.Sprintf("role token budget ROUTE_ROLE_TOKENS=%d reached for %s (%d used)", l, base, u.RoleTokens))
		}
	}
	b.Attempts = &Att{c.attempts, attempts, "hard"}
	if l := limit("ROUTE_TIMEOUT_S"); isInt(g("ROUTE_TIMEOUT_S")) && l != 0 {
		d.TimeoutS, b.TimeoutS, b.TimeoutEnforce = l, &l, "hard"
		if !in.HasTimeout {
			b.TimeoutEnforce = "advisory"
			warn("TIMEOUT_ADVISORY: no timeout/gtimeout command, so ROUTE_TIMEOUT_S is only recorded, not enforced")
		}
	}
	if x := g("ROUTE_BUDGET_COST"); x != "" {
		l := pos(x)
		b.Cost = &Cost{LimitUSD: l, EstimatedUSD: u.Est, EstimateComplete: u.Unpriced == 0, HostEstimateUSD: u.HostEst, HostRuns: u.HostRuns,
			HostProvenance: "the host's own client-side estimate, not a billed amount", Enforcement: "advisory"}
		if u.HostEst != nil && *u.HostEst >= l {
			warn(fmt.Sprintf("COST_HOST_ESTIMATE_OVER_BUDGET: the host reports an estimated $%s of $%s (its own client-side estimate, not a bill; advisory, not a stop)", fnum(*u.HostEst), x))
		}
		switch {
		case u.Est == nil && u.HostEst == nil:
			warn("COST_UNMEASURABLE: no priced usage yet (needs token usage and .sdkit/pricing.json); ROUTE_BUDGET_COST is not evaluated")
		case u.Est != nil && *u.Est >= l:
			warn(fmt.Sprintf("COST_ESTIMATE_OVER_BUDGET: estimated $%s of $%s (advisory; not a stop)", fnum(*u.Est), x))
		}
	}
	if in.get("ROUTE_COST_ENFORCE", "off") == "hard" {
		hfail := func(m string) Decision {
			note("ROUTE_COST_ENFORCE=hard fails closed: " + m)
			d.Budget = Budget{}
			return stop("CONFIG_INVALID")
		}
		var rate pricing.Rate
		priced := false
		if in.Hard.Rate != nil && d.Model != "" {
			rate, priced = in.Hard.Rate(d.Model)
		}
		resv, basis := pos(g("ROUTE_RESERVE_USD")), "ROUTE_RESERVE_USD"
		if g("ROUTE_RESERVE_USD") == "" {
			resv = 0
			cap := limit("ROUTE_ROLE_TOKENS")
			if cap == 0 {
				cap = limit("TOKEN_BUDGET_TASK")
			}
			if cap > 0 && priced {
				mr := max(rate.In, rate.Out, rate.CacheRead, rate.CacheWrite)
				resv, _ = strconv.ParseFloat(strconv.FormatFloat(mr*float64(cap)/1e6, 'f', 6, 64), 64)
				basis = fmt.Sprintf("derived: token cap %d x the dearest rate of %s (every token priced at it)", cap, d.Model)
			}
		}
		if resv <= 0 {
			return hfail("no usable reservation: set ROUTE_RESERVE_USD to a number > 0 (USD held per running role), or set ROUTE_ROLE_TOKENS/TOKEN_BUDGET_TASK and a price for the model so one can be derived")
		}
		if in.Hard.OpenErr {
			return hfail("open reservations could not be read")
		}
		h := &CostHard{Enforcement: "hard", ReservationUSD: resv, Basis: basis, Estimate: "estimate from the user price table, not a bill"}
		for _, sc := range []string{"task", "role", "project"} {
			lim := g(map[string]string{"task": "ROUTE_BUDGET_COST", "role": "ROUTE_ROLE_BUDGET_COST", "project": "ROUTE_PROJECT_BUDGET_COST"}[sc])
			if lim == "" {
				continue
			}
			sp, l := in.Hard.Spend[sc], pos(lim)
			s := &Scope{l, sp.USD, sp.Unknown, in.Hard.Open, resv, max(l-sp.USD-in.Hard.Open-resv, 0)}
			switch sc {
			case "task":
				h.Task = s
			case "role":
				h.Role = s
			default:
				h.Project = s
			}
			if sp.Unknown > 0 {
				over = append(over, fmt.Sprintf("%s cost limit unknown: %d earlier run(s) have no usage or no price, so spend cannot be shown to be under $%s (hard policy: unknown does not pass)", sc, sp.Unknown, lim))
			} else if sp.USD+in.Hard.Open+resv > l {
				over = append(over, fmt.Sprintf("%s cost limit $%s reached (estimated $%s + reserved $%s + this run $%s)", sc, lim, fnum(sp.USD), fnum(in.Hard.Open), fnum(resv)))
			}
		}
		if !priced && (g("ROUTE_BUDGET_COST") != "" || g("ROUTE_ROLE_BUDGET_COST") != "" || g("ROUTE_PROJECT_BUDGET_COST") != "") {
			over = append(over, "no price for model '"+orDef(d.Model, "(host default)")+"' in .sdkit/pricing.json: its cost is unknown, and unknown does not pass a hard cost limit")
		}
		b.CostHard = h
	}
	b.Accounting = acct
	if len(over) > 0 && mode != "off" {
		o := strings.Join(over, "; ")
		switch {
		case in.IgnoreBudget && mode == "strict":
			warn("BUDGET_OVERRIDDEN: " + o + " (--ignore-budget after the user agreed)")
		case mode == "guided":
			warn("BUDGET_REACHED: " + o + " (SDD_MODE=guided: warning only)")
		default:
			note(o)
			note("state is preserved; raise the limit in .sdkit/sdkit.local.env or ask the user and re-run with --ignore-budget; a mandatory role is never skipped")
			return stop("BUDGET_EXHAUSTED")
		}
	}
	// optional roles are not run once the budget is at its warning level (mandatory and work roles still are)
	if (d.RoleClass == "lookup" || d.RoleClass == "gate" && !d.Mandatory) && warnPct && mode == "strict" && in.Rerun == "" {
		return stop("OPTIONAL_SKIPPED", "optional role "+base+" not run: the task token budget is at its warning level")
	}
	return *d
}
