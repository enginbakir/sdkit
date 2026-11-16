package report

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/telemetry"
	"github.com/wecii/sdkit/internal/usage"
)

// Rec is one record of the routing, context or selection files (one JSON object per line).
type Rec = map[string]any

// Meta is what the caller knows about the report being built.
type Meta struct {
	Slug, Run, Key, Status, StatusSource, Now, BaselineModel string
	Terminal, BaseMissing, Regenerated                       bool
	Profile, Risk                                            *string
	WallS                                                    *float64
	Sessions                                                 []string
	Files                                                    *FilesChanged
	Purposes                                                 map[string]string
}

// TaskInputs is everything a task report is computed from: the usage report, the telemetry summary (nil when telemetry is off or empty),
// the routing, context and selection records, and the role statistics of earlier stored reports (the comparable-run baselines).
type TaskInputs struct {
	U          *usage.Result
	T          *telemetry.Summary
	RT, CX, SL []Rec
	Hist       [][]TaskRole
	M          Meta
}

var ctl = regexp.MustCompile(`[\p{Cc}]`)

// clean drops control characters and cuts a value shown to people at 120 characters.
func clean(s string) string {
	s = ctl.ReplaceAllString(s, "")
	if r := []rune(s); len(r) > 120 {
		return string(r[:120])
	}
	return s
}

// cleanmsg is clean for a sentence: cut at a word boundary and marked, never mid-word.
func cleanmsg(s string) string {
	s = ctl.ReplaceAllString(s, "")
	if r := []rune(s); len(r) > 400 {
		t := string(r[:400])
		if i := strings.LastIndex(t, " "); i >= 0 {
			t = t[:i]
		}
		return t + "…"
	}
	return s
}

func rowsOf(r *usage.Result) []usage.Row {
	if r == nil {
		return nil
	}
	rows := make([]usage.Row, len(r.Rows))
	for i, x := range r.Rows {
		x.Role, x.Model, x.Provider, x.Host, x.Session = clean(x.Role), clean(x.Model), clean(x.Provider), clean(x.Host), clean(x.Session)
		rows[i] = x
	}
	return rows
}

func fresh(r usage.Row) int { return r.In + r.Out + r.CacheWrite }
func tot(r usage.Row) int   { return r.In + r.Out + r.CacheRead + r.CacheWrite }

func measured(rows []usage.Row) (m []usage.Row) {
	for _, r := range rows {
		if r.TQ != "unavailable" {
			m = append(m, r)
		}
	}
	return m
}

func sum[T any](xs []T, f func(T) int) (n int) {
	for _, x := range xs {
		n += f(x)
	}
	return n
}

// nruns counts distinct execution ids; a row without an id is its own run.
func nruns(rows []usage.Row) int {
	ids, n := map[string]bool{}, 0
	for _, r := range rows {
		if r.ID == "" {
			n++
		} else {
			ids[r.ID] = true
		}
	}
	return n + len(ids)
}

func uniq(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		out = append(out, x)
	}
	sort.Strings(out)
	j := 0
	for i, x := range out {
		if i == 0 || x != out[i-1] {
			out[j] = x
			j++
		}
	}
	return out[:j]
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func baseRole(r string) string { return strings.TrimSuffix(r, "-deep") }

func kindOf(g []usage.Row) string {
	if g[0].Role == "orchestrator" {
		return "orchestrator"
	}
	for _, r := range g {
		if strings.HasPrefix(r.ID, "rr-") {
			return "host-agent"
		}
	}
	return "subagent"
}

func roleTable(rows []usage.Row) []RoleRow {
	sorted := append([]usage.Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Provider < b.Provider
	})
	out := []RoleRow{}
	for i := 0; i < len(sorted); {
		j := i
		for j < len(sorted) && sorted[j].Role == sorted[i].Role && sorted[j].Model == sorted[i].Model && sorted[j].Provider == sorted[i].Provider {
			j++
		}
		g := sorted[i:j]
		i = j
		var m, p []usage.Row
		var reasons []string
		for _, r := range g {
			if r.TQ != "unavailable" {
				m = append(m, r)
			}
			if r.CostStatus == "priced" {
				p = append(p, r)
			} else {
				reasons = append(reasons, r.CostReason)
			}
		}
		x := RoleRow{Role: g[0].Role, Kind: kindOf(g), Model: strp(g[0].Model), Provider: strp(g[0].Provider), Runs: nruns(g), UnmeasuredRun: len(g) - len(m),
			CostComplete: len(p) == len(g), CostReason: strings.Join(uniq(reasons), ",")}
		if len(m) > 0 {
			x.In, x.Out = ptr(sum(m, func(r usage.Row) int { return r.In })), ptr(sum(m, func(r usage.Row) int { return r.Out }))
			x.CacheRead, x.CacheWrite = ptr(sum(m, func(r usage.Row) int { return r.CacheRead })), ptr(sum(m, func(r usage.Row) int { return r.CacheWrite }))
			x.Tokens = ptr(sum(m, tot))
		}
		if len(p) > 0 {
			c := 0.0
			for _, r := range p {
				c += *r.Cost
			}
			x.CostUSD = ptr(r4(c))
		}
		out = append(out, x)
	}
	rank := func(r RoleRow) int {
		if r.Kind == "orchestrator" {
			return 0
		}
		return 1
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		return deref(a.Model) < deref(b.Model)
	})
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// routing prices the same measured tokens of every non-orchestrator run on the baseline model; a run on a dearer model counts negative.
func routing(rows []usage.Row, bm string) Routing {
	if bm == "" {
		return Routing{State: "na", Reason: "no REPORT_BASELINE_MODEL configured"}
	}
	var c, ok []usage.Row
	for _, r := range measured(rows) {
		if r.Role != "orchestrator" {
			c = append(c, r)
			if r.CostStatus == "priced" && r.AsStatus == "priced" {
				ok = append(ok, r)
			}
		}
	}
	if len(c) == 0 {
		return Routing{State: "na", Reason: "no measured agent run"}
	}
	if len(ok) == 0 {
		var rs []string
		for _, r := range c {
			if r.AsReason != "" { // the bash `.as_reason // .cost_reason`: an empty as_reason is kept as empty, not replaced by the cost reason
				rs = append(rs, r.AsReason)
			}
		}
		return Routing{State: "na", Reason: fmt.Sprintf("baseline model %s or the selected models are not priced (%s)", bm, strings.Join(uniq(rs), ","))}
	}
	o := Routing{State: "ok", BaselineModel: bm, Runs: len(c), PricedRuns: len(ok)}
	var models []string
	for _, r := range ok {
		a, b := *r.Cost, *r.AsCost
		if r.Model == bm {
			o.OnBaseline++
		}
		if b > a {
			o.Cheaper++
		}
		if b < a {
			o.Dearer++
		}
		o.ActualUSD += a
		o.BaselineUSD += b
		o.DeltaUSD += b - a
		models = append(models, r.Model)
	}
	o.Models = uniq(models)
	return o
}

func str(m Rec, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func fnum(m Rec, k string) float64 { f, _ := m[k].(float64); return f }

func sumRec(rs []Rec, k string) (n float64) {
	for _, r := range rs {
		n += fnum(r, k)
	}
	return n
}

// uniqueByID is jq's unique_by(.id): sorted by id, the first record of each.
func uniqueByID(rs []Rec) []Rec {
	s := append([]Rec(nil), rs...)
	sort.SliceStable(s, func(i, j int) bool { return str(s[i], "id") < str(s[j], "id") })
	out := []Rec{}
	for i, r := range s {
		if i == 0 || str(r, "id") != str(s[i-1], "id") {
			out = append(out, r)
		}
	}
	return out
}

func decisions(rt []Rec) []Rec {
	var d []Rec
	for _, r := range rt {
		if str(r, "type") == "decision" {
			d = append(d, r)
		}
	}
	return uniqueByID(d)
}

func avoidCls(d Rec) string {
	switch r := str(d, "reason"); {
	case str(d, "action") == "reuse":
		return "evidence_reuse"
	case r == "DUPLICATE_COMPLETED" || r == "DUPLICATE_IN_FLIGHT":
		return "skipped_redundant"
	case r == "OPTIONAL_SKIPPED":
		return "skipped_budget"
	}
	return ""
}

func conf(items []Item) string {
	for _, i := range items {
		if deref(i.Confidence) == "low" {
			return "low"
		}
	}
	return "medium"
}

// avoided counts the executions routing avoided and prices one only where a comparable recorded run of the same role exists: first
// this task, then earlier stored reports. No comparable record: counted, not priced.
func avoided(rt []Rec, rows []usage.Row, hist [][]TaskRole) Avoided {
	av := Avoided{ByClass: map[string]int{}, Items: []Item{}}
	var meas []usage.Row
	for _, r := range measured(rows) {
		if r.Role != "orchestrator" {
			meas = append(meas, r)
		}
	}
	for _, d := range decisions(rt) {
		cls := avoidCls(d)
		if cls == "" {
			continue
		}
		role := str(d, "role")
		av.Count++
		av.ByClass[cls]++
		it := Item{ID: str(d, "id"), Role: role, Cls: cls}
		var m []usage.Row
		seen := map[string]bool{}
		sm := append([]usage.Row(nil), meas...)
		sort.SliceStable(sm, func(i, j int) bool { return sm[i].ID < sm[j].ID })
		for _, r := range sm {
			if (r.Role == role || r.Role == baseRole(role)) && !seen[r.ID] {
				seen[r.ID] = true
				m = append(m, r)
			}
		}
		var h []TaskRole
		for _, rs := range hist {
			for _, r := range rs {
				if (r.Role == role || r.Role == baseRole(role)) && r.Runs > 0 && r.Tokens != nil {
					h = append(h, r)
				}
			}
		}
		switch {
		case len(m) > 0:
			n := float64(len(m))
			it.Source, it.Confidence = ptr(fmt.Sprintf("this task: %d recorded run(s) of the role", len(m))), ptr("medium")
			it.In, it.Out = ptr(float64(sum(m, func(r usage.Row) int { return r.In }))/n), ptr(float64(sum(m, func(r usage.Row) int { return r.Out }))/n)
			it.CacheRead, it.CacheWrite = ptr(float64(sum(m, func(r usage.Row) int { return r.CacheRead }))/n), ptr(float64(sum(m, func(r usage.Row) int { return r.CacheWrite }))/n)
			c, k := 0.0, 0
			for _, r := range m {
				if r.CostStatus == "priced" {
					c += *r.Cost
					k++
				}
			}
			if k == len(m) {
				it.CostUSD = ptr(c / n)
			}
		case len(h) > 0:
			runs := float64(sum(h, func(r TaskRole) int { return r.Runs }))
			it.Source, it.Confidence = ptr(fmt.Sprintf("earlier reports: %d run(s) of the role", int(runs))), ptr("low")
			d0 := func(p *int) float64 {
				if p == nil {
					return 0
				}
				return float64(*p)
			}
			var in, out, cr, cw, cost float64
			priced := true
			for _, r := range h {
				in, out, cr, cw = in+d0(r.In), out+d0(r.Out), cr+d0(r.CacheRead), cw+d0(r.CacheWrite)
				if r.CostUSD == nil || !r.CostComplete {
					priced = false
				} else {
					cost += *r.CostUSD
				}
			}
			it.In, it.Out, it.CacheRead, it.CacheWrite = ptr(in/runs), ptr(out/runs), ptr(cr/runs), ptr(cw/runs)
			if priced {
				it.CostUSD = ptr(cost / runs)
			}
		}
		it.Quantified = it.In != nil
		av.Items = append(av.Items, it)
		if it.Quantified {
			av.Quantified++
		}
	}
	return av
}

// savings keeps tokens and dollars apart. Baseline = what the same scope would have used or cost without the measured SDKit effect;
// N/A when nothing supports one.
func savings(rows []usage.Row, rt Routing, av Avoided) (SavTokens, SavCost) {
	actualFresh := float64(sum(measured(rows), fresh))
	var q []Item
	avFresh := 0.0
	for _, i := range av.Items {
		if i.In != nil {
			q = append(q, i)
			avFresh += *i.In + *i.Out + *i.CacheWrite
		}
	}
	var st SavTokens
	if len(q) == 0 {
		why := fmt.Sprintf("%d avoided execution(s) counted, none has a comparable recorded run", av.Count)
		if av.Count == 0 {
			why = "no avoided execution observed (routing changes the price, not the token count)"
		}
		st = SavTokens{State: "na", Text: "N/A — insufficient baseline data", Why: why}
	} else {
		var srcs []string
		for _, i := range q {
			srcs = append(srcs, deref(i.Source))
		}
		st = SavTokens{State: "estimated", Kind: "estimated", BaselineTokens: roundf(actualFresh + avFresh), ActualTokens: roundf(actualFresh), SavedTokens: roundf(avFresh),
			BaselineSource: strings.Join(uniq(srcs), "; "), Method: "avoided executions x mean tokens of the comparable recorded runs; baseline = actual + avoided",
			Accounting: "fresh tokens = in + out + cache_write; cache reads are not counted as saved or as spent here",
			Coverage:   fmt.Sprintf("%d of %d avoided execution(s) quantified", len(q), av.Count), Confidence: conf(q)}
		if actualFresh+avFresh > 0 {
			st.RatePct = ptr(r1(avFresh / (actualFresh + avFresh) * 100))
		}
	}
	var rd, ad *float64
	if rt.State == "ok" {
		rd = ptr(rt.DeltaUSD)
	}
	var asrc []string
	for _, i := range q {
		if i.CostUSD != nil {
			if ad == nil {
				ad = ptr(0.0)
			}
			*ad += *i.CostUSD
			asrc = append(asrc, deref(i.Source))
		}
	}
	if rd == nil && ad == nil {
		why := "routing not applicable"
		if rt.State == "na" {
			why = rt.Reason
		}
		return st, SavCost{State: "na", Text: "N/A — insufficient baseline data", Why: why}
	}
	act, priced := 0.0, 0
	for _, r := range rows {
		if r.CostStatus == "priced" {
			act += *r.Cost
			priced++
		}
	}
	base := act + deref0(rd) + deref0(ad)
	c := SavCost{State: "estimated", Kind: "estimated", BaselineUSD: r4(base), ActualUSD: r4(act), SavedUSD: r4(base - act), Method: "baseline cost = actual priced cost + routing counterfactual + avoided executions; savings = baseline - actual",
		Coverage: fmt.Sprintf("%d of %d run(s) priced", priced, len(rows)), Regression: base-act < 0, Components: []Component{}}
	if rt.State == "ok" {
		c.Coverage += fmt.Sprintf("; routing on %d run(s)", rt.PricedRuns)
	}
	if base > 0 {
		c.RatePct = ptr(r1((base - act) / base * 100))
	}
	if rd != nil {
		c.Components = append(c.Components, Component{"routing", r4(*rd), "same tokens priced on baseline model " + rt.BaselineModel, "low"})
	}
	if ad != nil {
		c.Components = append(c.Components, Component{"avoided executions", r4(*ad), strings.Join(uniq(asrc), "; "), conf(q)})
	}
	c.Confidence = "medium"
	if rd != nil {
		c.Confidence = "low"
	}
	return st, c
}

func deref0(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func roundf(x float64) float64 { return math.Round(x) }

func lenOf(v any) int {
	switch x := v.(type) {
	case map[string]any:
		return len(x)
	case []any:
		return len(x)
	case string:
		return len([]rune(x))
	case float64:
		if x < 0 {
			return int(-x)
		}
		return int(x)
	}
	return 0
}

func optimizations(t *telemetry.Summary, rt, cx, sl []Rec, av Avoided, rtg Routing) []Optimization {
	o := []Optimization{}
	if rtg.State == "ok" {
		b := ""
		if rtg.DeltaUSD >= 0 {
			b = "$" + num(r4(rtg.DeltaUSD)) + " avoided"
		} else {
			b = "REGRESSION $" + num(r4(-rtg.DeltaUSD)) + " extra"
		}
		o = append(o, Optimization{"Intelligent routing", "estimated",
			fmt.Sprintf("%d priced agent run(s) on %s; %d on the baseline model, %d cheaper, %d dearer", rtg.PricedRuns, strings.Join(rtg.Models, ", "), rtg.OnBaseline, rtg.Cheaper, rtg.Dearer),
			b + " vs baseline " + rtg.BaselineModel + " (same-token counterfactual)"})
	}
	if n := av.ByClass["evidence_reuse"]; n > 0 {
		s := "; tokens not quantified (no comparable run)"
		if av.Quantified > 0 {
			s = "; priced only where a comparable run exists (see savings)"
		}
		o = append(o, Optimization{"Evidence reuse", "measured count", fmt.Sprintf("%d valid evidence reuse(s) recorded by routing", n), fmt.Sprintf("%d execution(s) avoided", n) + s})
	}
	if n := av.ByClass["skipped_redundant"] + av.ByClass["skipped_budget"]; n > 0 {
		s := "; tokens not quantified"
		if av.Quantified > 0 {
			s = "; priced only where a comparable run exists"
		}
		o = append(o, Optimization{"Skipped redundant agents", "measured count", fmt.Sprintf("%d duplicate and %d budget-skipped request(s)", av.ByClass["skipped_redundant"], av.ByClass["skipped_budget"]),
			fmt.Sprintf("%d execution(s) not started", n) + s})
	}
	if c, s := sumRec(sl, "cached"), sumRec(sl, "skipped_suites"); c > 0 || s > 0 {
		o = append(o, Optimization{"Test selection", "measured", fmt.Sprintf("ran %s, reused %s cached green, left out %s suite(s)", num(sumRec(sl, "ran")), num(c), num(s)),
			num(sumRec(sl, "cached_s")) + "s of cached runs replayed (time measured when first run); skipped suites untimed; no token claim"})
	}
	if b := sumRec(cx, "full_bytes") - sumRec(cx, "shown_bytes"); b > 0 {
		o = append(o, Optimization{"Context optimization", "heuristic", fmt.Sprintf("%d bounded view(s); %s bytes kept off the orchestrator context", len(cx), num(b)),
			fmt.Sprintf("about %s tokens at ~4 bytes/token (heuristic, not in the savings rate)", num(float64(int64(b/4))))})
	}
	var bd []Rec
	for _, r := range rt {
		if str(r, "type") == "decision" && lenOf(r["budget"]) > 0 {
			bd = append(bd, r)
		}
	}
	if bd = uniqueByID(bd); len(bd) > 0 {
		var ex []Rec
		for _, r := range rt {
			if str(r, "type") == "decision" && str(r, "reason") == "BUDGET_EXHAUSTED" {
				ex = append(ex, r)
			}
		}
		o = append(o, Optimization{"Budget enforcement", "qualitative", fmt.Sprintf("%d routing decision(s) with budget accounting; %d stopped for budget", len(bd), len(uniqueByID(ex))),
			"spending checked before each run; no dollar claim"})
	}
	if t != nil && (len(t.Phases) > 1 || t.Failures.GateRejections > 0) {
		b := "transitions validated; nothing had to be stopped"
		if g := t.Failures.GateRejections; g > 0 {
			b = fmt.Sprintf("%d invalid move(s) stopped before they landed", g)
		}
		o = append(o, Optimization{"Workflow enforcement", "qualitative", fmt.Sprintf("%d phase(s), %d gate rejection(s)", len(t.Phases), t.Failures.GateRejections), b})
	}
	return o
}

func purposeOf(p map[string]string, r string) *string {
	if v, ok := p[r]; ok {
		return &v
	}
	if v, ok := p[baseRole(r)]; ok {
		return &v
	}
	return nil
}

func tokensOf(rows, meas []usage.Row, u *usage.Result) Tokens {
	if len(rows) == 0 {
		return Tokens{State: "unavailable", Note: "no usage ledger rows (USAGE_LOG off, or nothing recorded)"}
	}
	t := Tokens{State: "recorded", In: ptr(sum(meas, func(r usage.Row) int { return r.In })), Out: ptr(sum(meas, func(r usage.Row) int { return r.Out })),
		CacheRead: ptr(sum(meas, func(r usage.Row) int { return r.CacheRead })), CacheWrite: ptr(sum(meas, func(r usage.Row) int { return r.CacheWrite })),
		Total: ptr(sum(meas, tot)), Fresh: ptr(sum(meas, fresh)), MeasuredRuns: ptr(nruns(meas)), CacheHitRate: u.Cache.HitRate,
		Note: "cache reads are input served from cache, never counted as newly generated tokens"}
	var un []usage.Row
	for _, r := range rows {
		if r.TQ == "unavailable" {
			un = append(un, r)
		}
	}
	t.UnmeasuredRuns = ptr(nruns(un))
	if len(un) > 0 {
		t.State = "partial"
	}
	return t
}

var reasonRe = regexp.MustCompile(`not priced|no pricing|unreadable`)

func costOf(rows []usage.Row, u *usage.Result) Cost {
	priced, any := 0, false
	for _, r := range rows {
		if r.CostStatus == "priced" {
			priced++
		} else {
			any = true
		}
	}
	if len(rows) == 0 {
		return Cost{State: "unavailable", short: true}
	}
	c := Cost{State: "estimated", APIEquivalent: rp4(u.Totals.Cost), PricedRuns: u.Totals.Priced, UnpricedRuns: u.Totals.Unpriced, PricingSource: u.Pricing.Source,
		Billed:       "unavailable: no source reports it; a subscription is not billed per token, the figure is what the same tokens cost at API rates",
		HostEstimate: rp4(u.HostEstimate.USD), CacheSaving: rp4(u.Cache.SavingUSD), CacheSavingTxt: "provider prompt caching vs uncached input, API-equivalent; not an SDKit optimization", Reasons: []string{}}
	if priced == 0 {
		c.State = "unavailable"
	} else if any {
		c.State = "partial"
	}
	for _, w := range u.Warnings {
		if reasonRe.MatchString(w) {
			c.Reasons = append(c.Reasons, w)
		}
	}
	return c
}

func usageWarnings(u *usage.Result) []string {
	if u == nil {
		return nil
	}
	return u.Warnings
}

func parseISO(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	return t, err == nil
}

func durText(s *float64) string {
	if s == nil {
		return "N/A"
	}
	n := int64(*s)
	if *s < 0 && float64(n) != *s {
		n--
	}
	switch {
	case n < 60:
		return fmt.Sprintf("%ds", n)
	case n < 3600:
		return fmt.Sprintf("%dm%ds", n/60, n%60)
	}
	return fmt.Sprintf("%dh%dm", n/3600, (n%3600)/60)
}

// buildTask computes a task report. u may be nil (no ledger line at all).
func buildTask(in TaskInputs) *TaskReport {
	u, t, m := in.U, in.T, in.M
	if u == nil {
		u = &usage.Result{}
	}
	rows := rowsOf(in.U)
	meas := measured(rows)
	roles := roleTable(rows)
	rtg := routing(rows, m.BaselineModel)
	av := avoided(in.RT, rows, in.Hist)
	st, sc := savings(rows, rtg, av)
	dec := decisions(in.RT)
	var tests []telemetry.TestCount
	if t != nil {
		tests = t.Tests
	}
	var tp, tf, tr int
	for _, x := range tests {
		switch {
		case x.Outcome == "pass":
			tp += x.Runs
		case x.Outcome == "fail" && x.Phase != "red":
			tf += x.Runs
		case x.Outcome == "fail":
			tr += x.Runs
		}
	}
	var vf *string
	if t != nil {
		vf = t.Verification.Final
	}
	status := m.Status
	if t != nil && status == "completed" && t.Contracts.ByStatus["partial"]+t.Contracts.ByStatus["blocked"]+t.Contracts.ByStatus["failed"] > 0 {
		status = "partial"
	}
	var hosts []string
	for _, r := range rows {
		if r.Host != "" {
			hosts = append(hosts, r.Host)
		}
	}
	hosts = uniq(hosts)
	var pm *string
	best := -1
	tokBy := map[string]int{}
	var models []string
	for _, r := range meas {
		if r.Model != "" {
			tokBy[r.Model] += tot(r)
			models = append(models, r.Model)
		}
	}
	for _, mo := range uniq(models) {
		if tokBy[mo] > best {
			best, pm = tokBy[mo], ptr(mo)
		}
	}
	var lts []string
	for _, r := range meas {
		if r.TS != "" {
			lts = append(lts, r.TS)
		}
	}
	sort.Strings(lts)
	dur := Duration{Source: "unavailable"}
	switch {
	case t != nil:
		dur = Duration{ptr(t.WallS), "telemetry events"}
	case m.WallS != nil:
		dur = Duration{m.WallS, "task state"}
	case len(lts) > 1:
		a, ok1 := parseISO(lts[0])
		b, ok2 := parseISO(lts[len(lts)-1])
		if ok1 && ok2 {
			dur = Duration{ptr(b.Sub(a).Seconds()), "usage record timestamps (lower bound)"}
		}
	}
	task := TaskInfo{Name: clean(m.Slug), ID: clean(m.Slug), Run: m.Run, Status: status, StatusRequested: m.Status, Terminal: m.Terminal, StatusSource: m.StatusSource, Duration: dur,
		Profile: m.Profile, Risk: m.Risk, Hosts: hosts, PrimaryModel: pm, Sessions: orEmpty(m.Sessions), FilesChanged: m.Files}
	if t != nil {
		if t.Workflow.Profile != nil {
			task.Profile = t.Workflow.Profile
		}
		if t.Risk != nil {
			task.Risk = t.Risk
		}
	}
	if len(tests) == 0 {
		task.Tests = Tests{State: "not recorded"}
	} else {
		task.Tests = Tests{State: "passed", Passed: &tp, Failed: &tf, RedExpected: &tr}
		if tf > 0 {
			task.Tests.State = "failed"
		}
	}
	rej := 0
	if t != nil {
		rej = t.Failures.GateRejections
	}
	switch {
	case !m.Terminal:
		task.QualityGates = Gate{State: "Pending", Note: ptr("task still open: the gate runs when it is verified")}
	case t == nil:
		task.QualityGates = Gate{State: "Not run", Note: ptr("telemetry off: no gate result recorded")}
	case vf == nil:
		task.QualityGates = Gate{State: "Not run", Note: ptr("no verification result recorded")}
	case strings.ToLower(*vf) == "pass":
		task.QualityGates = Gate{State: "Passed", Final: vf, Rejections: &rej}
	default:
		task.QualityGates = Gate{State: "Failed", Final: vf, Rejections: &rej}
	}
	tr2 := make([]TaskRole, 0, len(roles))
	byKind := map[string]int{}
	var roleNames []string
	for _, r := range roles {
		why := []string{}
		for _, d := range dec {
			if dr := str(d, "role"); dr == r.Role || baseRole(dr) == r.Role {
				why = append(why, str(d, "reason"))
			}
		}
		res := "recorded"
		switch {
		case r.Role == "verifier" || r.Role == "verifier-deep":
			res = "no verdict recorded"
			if vf != nil {
				res = *vf
			}
		case r.Kind == "orchestrator":
			res = "coordinated"
		}
		tr2 = append(tr2, TaskRole{r, purposeOf(m.Purposes, r.Role), uniq(why), res})
		byKind[r.Kind] += r.Runs
		roleNames = append(roleNames, r.Role)
	}
	unm := 0
	for _, r := range rows {
		if r.TQ == "unavailable" {
			unm++
		}
	}
	val := Validation{Tests: []string{}, Verification: []string{}, Final: vf, Failures: nil}
	for _, x := range tests {
		val.Tests = append(val.Tests, fmt.Sprintf("%s %s x%d", x.Phase, x.Outcome, x.Runs))
	}
	if t != nil {
		for _, v := range t.Verification.Results {
			val.Verification = append(val.Verification, v.Outcome)
		}
		val.Retries, val.Failures = ptr(t.Retries), t.Failures.ByCategory
	}
	warn := append([]string{}, usageWarnings(in.U)...)
	if t == nil {
		warn = append(warn, "telemetry off or empty: phases, durations, tests and gate results are unavailable")
	}
	if m.BaseMissing {
		warn = append(warn, "REPORT_BASELINE_MODEL is not set: routing savings are N/A")
	}
	for i := range warn {
		warn[i] = cleanmsg(warn[i])
	}
	sum_ := clean(m.Slug) + " " + map[bool]string{true: "ended", false: "is"}[m.Terminal] + " " + strings.ToUpper(status)
	if dur.S != nil {
		sum_ += " after " + durText(dur.S)
	}
	sum_ += fmt.Sprintf(": %d role execution(s) across %d role(s)", nruns(rows), len(uniq(roleNames)))
	if vf != nil {
		sum_ += "; final verification " + *vf
	}
	if status == "partial" {
		sum_ += "; some role results were partial, blocked or failed"
	}
	rep := &TaskReport{Schema: "value-report/1", Kind: "task", Key: m.Key, GeneratedAt: m.Now, Regenerated: m.Regenerated, Task: task, Roles: tr2,
		Executions: Executions{Total: nruns(rows), ByKind: byKind, Unmeasured: unm}, Tokens: tokensOf(rows, meas, u), Cost: costOf(rows, u),
		Savings: Savings{st, sc, rtg, av}, Optimizations: optimizations(t, in.RT, in.CX, in.SL, av, rtg), Validation: val, Warnings: warn, Summary: sum_ + "."}
	return rep
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SessionInputs is what a session report is computed from: the usage report of the session, the stored task reports (index lines) that
// belong to it, and its state.
type SessionInputs struct {
	U                          *usage.Result
	Tasks                      []Index
	ID                         string
	State                      string
	Reason, Now, BaselineModel string
	EndedAt                    *string
	Key                        string
	Regenerated                bool
}

func buildSession(in SessionInputs) *SessionReport {
	u := in.U
	if u == nil {
		u = &usage.Result{}
	}
	rows := rowsOf(in.U)
	meas := measured(rows)
	roles := roleTable(rows)
	rtg := routing(rows, in.BaselineModel)
	var inside, span []Index
	for _, t := range in.Tasks {
		if len(t.Sessions) <= 1 {
			inside = append(inside, t)
		} else {
			span = append(span, t)
		}
	}
	avc := map[string]int{}
	at, ac := 0.0, 0.0
	for _, t := range inside {
		for k, n := range t.ByClass {
			avc[k] += n
		}
		if t.SavedTokens != nil {
			at += *t.SavedTokens
		}
		ac += t.SavedAvoid
	}
	s := &SessionReport{Schema: "value-report/1", Kind: "session", Key: in.Key, GeneratedAt: in.Now, Regenerated: in.Regenerated, Roles: roles,
		Tokens: tokensOf(rows, meas, u), Cost: costOf(rows, u), EvidenceReuse: avc["evidence_reuse"], Skipped: avc["skipped_redundant"] + avc["skipped_budget"], Warnings: []string{}}
	if len(rows) == 0 {
		s.Tokens = Tokens{State: "unavailable"}
	}
	var first []string
	for _, r := range rows {
		if r.TS != "" {
			first = append(first, r.TS)
		}
	}
	sort.Strings(first)
	si := SessionInfo{ID: clean(in.ID), State: in.State, EndReason: clean(in.Reason), EndedAt: in.EndedAt}
	if len(first) > 0 {
		si.FirstRecord, si.LastRecord = &first[0], &first[len(first)-1]
	}
	switch in.State {
	case "ended":
		si.Note = "true end-of-session signal received"
	case "interrupted":
		si.Note = "no end signal: the session was killed or abandoned; usage is what was recorded up to then"
	default:
		si.Note = "still running, or no end signal yet"
	}
	s.Session = si
	tk := SessionTasks{Total: len(in.Tasks), List: []SessionTask{}}
	gates := map[string]int{}
	var pass, fail int
	for _, t := range in.Tasks {
		switch t.Status {
		case "completed":
			tk.Completed++
		case "partial":
			tk.Partial++
		case "failed", "aborted":
			tk.Failed++
		case "blocked":
			tk.Blocked++
		}
		tk.List = append(tk.List, SessionTask{t.Task, t.Status, t.Tokens, t.Cost})
		gates[t.Gates]++
		pass, fail = pass+t.TestsPassed, fail+t.TestsFailed
	}
	s.Tasks = tk
	if len(in.Tasks) > 0 {
		s.Validation = SessionValidation{&pass, &fail, gates}
	} else {
		s.Validation = SessionValidation{Gates: gates}
	}
	byRole := map[string]*RoleRuns{}
	var names []string
	for _, r := range roles {
		x := byRole[r.Role]
		if x == nil {
			x = &RoleRuns{Role: r.Role}
			byRole[r.Role] = x
			names = append(names, r.Role)
		}
		x.Runs += r.Runs
	}
	sort.Strings(names)
	un := 0
	for _, r := range rows {
		if r.TQ == "unavailable" {
			un++
		}
	}
	ex := SessionExec{Total: nruns(rows), ByRole: []RoleRuns{}, Unmeasured: un}
	for _, n := range names {
		var ks []string
		for _, r := range roles {
			if r.Role == n {
				ks = append(ks, r.Kind)
			}
		}
		byRole[n].Kinds = uniq(ks)
		ex.ByRole = append(ex.ByRole, *byRole[n])
	}
	s.Executions = ex
	sv := SessionSavings{Routing: rtg, Avoided: SessionAvoided{Counted: avc, Excluded: fmt.Sprintf("%d task(s) span several sessions: their avoided executions stay in the task reports and are not added here", len(span))}}
	if rtg.State == "ok" {
		sv.RoutingUSD = ptr(r4(rtg.DeltaUSD))
	}
	if at > 0 {
		sv.Avoided.TokensEs = &at
		sv.Tokens = SessionSavTokens{State: "estimated", Saved: at, Note: "sum of the quantified avoided executions of tasks fully inside this session; rate is in each task report"}
	} else {
		sv.Tokens = SessionSavTokens{State: "na", Text: "N/A — insufficient baseline data"}
	}
	if ac > 0 {
		sv.Avoided.USDEs = &ac
	}
	if rtg.State == "ok" || ac > 0 {
		d := 0.0
		if rtg.State == "ok" {
			d = rtg.DeltaUSD
		}
		sv.Cost = SessionSavCost{State: "estimated", Saved: r4(d + ac), Routing: sv.RoutingUSD}
		if ac > 0 {
			sv.Cost.Avoided = ptr(r4(ac))
		}
	} else {
		why := rtg.Reason
		if why == "" {
			why = "none"
		}
		sv.Cost = SessionSavCost{State: "na", Text: "N/A — insufficient baseline data", Why: why}
	}
	s.Savings = sv
	opts := []string{}
	for _, t := range in.Tasks {
		opts = append(opts, t.Optims...)
	}
	s.Optimizations = uniq(opts)
	s.Warnings = append(s.Warnings, usageWarnings(in.U)...)
	if in.State != "ended" {
		s.Warnings = append(s.Warnings, "this session has no end signal: figures cover what was recorded")
	}
	for i := range s.Warnings {
		s.Warnings[i] = cleanmsg(s.Warnings[i])
	}
	s.Summary = fmt.Sprintf("session %s (%s): %d task(s), %d role execution(s)", clean(in.ID), in.State, len(in.Tasks), nruns(rows))
	if in.State != "ended" {
		s.Summary += " [not a confirmed session end]"
	}
	s.Summary += "."
	return s
}
