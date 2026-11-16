package usage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wecii/sdkit/internal/pricing"
)

// Row is one execution on one model after deduplication, priced at read time (a pricing change recalculates the whole history).
// CostStatus is priced, unpriced (no price for it: N/A) or unmeasured (the host gave no token usage).
type Row struct {
	ID         string   `json:"id"`
	Role       string   `json:"role"`
	Tier       string   `json:"tier"`
	Host       string   `json:"host"`
	Model      string   `json:"model"`
	Provider   string   `json:"provider"`
	Session    string   `json:"session"`
	Origin     string   `json:"origin"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	TQ         string   `json:"tq"`
	TS         string   `json:"ts"`
	In         int      `json:"in"`
	Out        int      `json:"out"`
	CacheRead  int      `json:"cache_read"`
	CacheWrite int      `json:"cache_write"`
	Cost       *float64 `json:"cost_usd"`
	CostStatus string   `json:"cost_status"`
	CostReason string   `json:"cost_reason"`
	AsCost     *float64 `json:"as_cost_usd,omitempty"` // with AsModel: the same run priced as if it ran on that model
	AsStatus   string   `json:"as_status,omitempty"`
	AsReason   string   `json:"as_reason,omitempty"`
	saving     *float64
	hostCost   *float64
}

// Agg is a sum over rows. Cost is nil unless at least one row is priced; Partial is set when any row is unpriced or unmeasured.
type Agg struct {
	Runs       int      `json:"runs"`
	Lines      int      `json:"lines"`
	In         int      `json:"in"`
	Out        int      `json:"out"`
	CacheRead  int      `json:"cache_read"`
	CacheWrite int      `json:"cache_write"`
	Unmeasured int      `json:"unmeasured"`
	Priced     int      `json:"priced"`
	Unpriced   int      `json:"unpriced"`
	Cost       *float64 `json:"cost_usd"`
	Partial    bool     `json:"partial"`
}

// Group is an Agg under one key (role+tier, model+provider or session).
type Group struct {
	Key1, Key2 string
	Agg
	names [2]string // json names of the keys
}

// MarshalJSON names the keys after what the group is by (role+tier, model+provider, session+origin).
func (g Group) MarshalJSON() ([]byte, error) {
	b, _ := json.Marshal(g.Agg)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	m[g.names[0]] = g.Key1
	m[g.names[1]] = g.Key2
	return json.Marshal(m)
}

// Result is the whole report.
type Result struct {
	Scope   string `json:"scope"`
	Pricing struct {
		Source   string  `json:"source"`
		Currency *string `json:"currency"`
	} `json:"pricing"`
	Totals    Agg     `json:"totals"`
	Rows      []Row   `json:"rows"`
	ByRole    []Group `json:"by_role"`
	ByModel   []Group `json:"by_model"`
	BySession []Group `json:"by_session"`
	ByOrigin  []struct {
		Origin string `json:"origin"`
		Runs   int    `json:"runs"`
	} `json:"by_origin"`
	Cache struct {
		HitRate   *float64 `json:"hit_rate"`
		InputSide int      `json:"input_side_tokens"`
		Read      int      `json:"read"`
		Write     int      `json:"write"`
		SavingUSD *float64 `json:"saving_usd"`
	} `json:"cache"`
	HostEstimate struct {
		USD  *float64 `json:"usd"`
		Runs int      `json:"runs"`
		Note string   `json:"note"`
	} `json:"host_estimate"`
	Coverage struct {
		LedgerLines int  `json:"ledger_lines"`
		Executions  int  `json:"executions"`
		Duplicates  int  `json:"duplicates_collapsed"`
		Unmeasured  int  `json:"unmeasured_runs"`
		Priced      int  `json:"priced_runs"`
		Unpriced    int  `json:"unpriced_runs"`
		Spawns      *int `json:"spawns_registered"`
	} `json:"coverage"`
	Warnings []string `json:"warnings"`
	Partial  bool     `json:"partial"`
}

// Options of Compute. Prices is the price table path ("" or missing: tokens only, cost N/A). Spawns is the registered spawn count of
// the task (nil: unknown). AsModel also prices every run as if it had run on that model (nothing is stored).
type Options struct {
	Scope, Prices, Session, AsModel string
	Spawns                          *int
}

func ptr(f float64) *float64 { return &f }

// Compute reads the ledger files and builds the report; nil when they hold no line at all.
func Compute(files []string, o Options) *Result {
	var all []Line
	for _, f := range files {
		all = append(all, ReadLines(f)...)
	}
	if len(all) == 0 {
		return nil
	}
	for i := range all {
		l := &all[i]
		l.Role, l.Tier, l.Origin, l.Kind, l.TQ = orStr(l.Role, "?"), orStr(l.Tier, "-"), orStr(l.Origin, "sdk"), orStr(l.Kind, "snapshot"), l.tq()
	}
	var tb *pricing.Table
	psrc := "none"
	if _, err := os.Stat(o.Prices); err == nil && o.Prices != "" {
		if t, e := pricing.Load(o.Prices); e == nil {
			tb, psrc = t, o.Prices
		} else {
			psrc = "unreadable: " + o.Prices
		}
	}
	r := &Result{Scope: o.Scope}
	var sel []Line
	for _, l := range all {
		if o.Session == "" || l.Session == o.Session {
			sel = append(sel, l)
		}
	}
	// one line per (id, model): final beats snapshot, then measured, then the larger total, then the later one; counters only grow
	measured := map[string]bool{}
	for _, l := range sel {
		if l.ID != "" && l.TQ != "unavailable" {
			measured[l.ID] = true
		}
	}
	groups := map[[2]string][]Line{}
	var anon []Line
	for _, l := range sel {
		switch {
		case l.ID == "":
			anon = append(anon, l)
		case l.TQ == "unavailable" && l.Model == "" && measured[l.ID]:
		default:
			k := [2]string{l.ID, l.Model}
			groups[k] = append(groups[k], l)
		}
	}
	keys := make([][2]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+"\x00"+keys[i][1] < keys[j][0]+"\x00"+keys[j][1] })
	var keep []Line
	for _, k := range keys {
		g := groups[k]
		rank := func(l Line) (int, int, int, string) {
			a, b := 0, 0
			if l.Kind == "final" {
				a = 1
			}
			if l.TQ != "unavailable" {
				b = 1
			}
			return a, b, l.Total(), l.TS
		}
		sort.SliceStable(g, func(i, j int) bool {
			a1, b1, t1, s1 := rank(g[i])
			a2, b2, t2, s2 := rank(g[j])
			if a1 != a2 {
				return a1 < a2
			}
			if b1 != b2 {
				return b1 < b2
			}
			if t1 != t2 {
				return t1 < t2
			}
			return s1 < s2
		})
		l := g[len(g)-1]
		if l.TQ != "unavailable" {
			for _, x := range g {
				l.In, l.Out, l.CacheRead, l.CacheWrite = maxNum(l.In, x.In), maxNum(l.Out, x.Out), maxNum(l.CacheRead, x.CacheRead), maxNum(l.CacheWrite, x.CacheWrite)
				if x.Kind == "final" {
					l.Kind = "final"
				}
			}
		}
		keep = append(keep, l)
	}
	for _, l := range append(keep, anon...) {
		r.Rows = append(r.Rows, price(l, tb))
	}
	if o.AsModel != "" {
		for i := range r.Rows {
			l := Line{TS: r.Rows[i].TS, Model: o.AsModel, TQ: r.Rows[i].TQ, In: Num(r.Rows[i].In), Out: Num(r.Rows[i].Out), CacheRead: Num(r.Rows[i].CacheRead), CacheWrite: Num(r.Rows[i].CacheWrite)}
			a := price(l, tb)
			r.Rows[i].AsCost, r.Rows[i].AsStatus, r.Rows[i].AsReason = a.Cost, a.CostStatus, a.CostReason
		}
	}
	ids := map[string]bool{}
	for _, x := range r.Rows {
		if x.ID != "" {
			ids[x.ID] = true
		}
	}
	r.Totals = agg(r.Rows)
	r.Pricing.Source = psrc
	if tb != nil {
		c := orStr(tb.Currency, "USD")
		r.Pricing.Currency = &c
	}
	// cache effectiveness over measured runs only
	read, input, write := 0, 0, 0
	missingSave := false
	var save float64
	for _, x := range r.Rows {
		if x.TQ == "unavailable" {
			continue
		}
		input += x.In + x.CacheRead + x.CacheWrite
		read, write = read+x.CacheRead, write+x.CacheWrite
		if x.CacheRead > 0 && x.saving == nil {
			missingSave = true
		}
		if x.saving != nil {
			save += *x.saving
		}
	}
	r.Cache.InputSide, r.Cache.Read, r.Cache.Write = input, read, write
	if input > 0 {
		r.Cache.HitRate = ptr(float64(read) / float64(input))
	}
	if !missingSave {
		r.Cache.SavingUSD = ptr(save)
	}
	var host float64
	for _, x := range r.Rows {
		if x.hostCost != nil {
			host += *x.hostCost
			r.HostEstimate.Runs++
		}
	}
	if r.HostEstimate.Runs > 0 {
		r.HostEstimate.USD = ptr(host)
	}
	r.HostEstimate.Note = "Claude Code client-side estimate, not billed"
	r.ByRole = group(r.Rows, [2]string{"role", "tier"}, func(x Row) (string, string) { return x.Role, x.Tier })
	r.ByModel = group(r.Rows, [2]string{"model", "provider"}, func(x Row) (string, string) { return orStr(x.Model, "(unknown)"), orStr(x.Provider, "-") })
	r.BySession = group(r.Rows, [2]string{"session", "origin"}, func(x Row) (string, string) { return orStr(x.Session, "(none)"), "" })
	for i := range r.BySession {
		origins := map[string]bool{}
		for _, x := range r.Rows {
			if orStr(x.Session, "(none)") == r.BySession[i].Key1 {
				origins[x.Origin] = true
			}
		}
		r.BySession[i].Key2 = strings.Join(sortedKeys(origins), "+")
	}
	counts := map[string]int{}
	for _, x := range r.Rows {
		counts[x.Origin]++
	}
	for _, k := range sortedKeys(counts) {
		r.ByOrigin = append(r.ByOrigin, struct {
			Origin string `json:"origin"`
			Runs   int    `json:"runs"`
		}{k, counts[k]})
	}
	agentIDs := map[string]bool{}
	for _, x := range r.Rows {
		if x.Role != "orchestrator" && x.ID != "" {
			agentIDs[x.ID] = true
		}
	}
	r.Coverage.LedgerLines, r.Coverage.Executions = len(sel), len(ids)+len(anon)
	r.Coverage.Duplicates, r.Coverage.Spawns = len(sel)-len(r.Rows), o.Spawns
	r.Coverage.Unmeasured, r.Coverage.Priced, r.Coverage.Unpriced = r.Totals.Unmeasured, r.Totals.Priced, r.Totals.Unpriced
	r.Warnings = warnings(r, tb, psrc, len(anon), len(agentIDs), o)
	r.Partial = r.Totals.Unpriced > 0 || len(r.Warnings) > 0
	return r
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// price resolves the model in the table; anything it cannot price says why instead of guessing a number.
func price(l Line, tb *pricing.Table) Row {
	x := Row{ID: l.ID, Role: l.Role, Tier: l.Tier, Host: l.Host, Model: l.Model, Provider: l.Provider, Session: l.Session, Origin: l.Origin, Kind: l.Kind,
		Status: l.Status, TQ: l.TQ, TS: l.TS, In: int(l.In), Out: int(l.Out), CacheRead: int(l.CacheRead), CacheWrite: int(l.CacheWrite), hostCost: l.CostEst}
	no := func(why string) Row { x.CostStatus, x.CostReason = "unpriced", why; return x }
	if l.TQ == "unavailable" {
		x.CostStatus, x.CostReason = "unmeasured", "tokens_unavailable"
		return x
	}
	if tb == nil {
		return no("no_pricing_table")
	}
	if len(tb.Models) == 0 {
		return no("pricing_table_has_no_models")
	}
	rt, ok := tb.Lookup(l.Model)
	if !ok {
		return no("unknown_model")
	}
	var miss []string
	for _, c := range []struct {
		n    string
		have int
		rate float64
	}{{"in", x.In, rt.In}, {"out", x.Out, rt.Out}, {"cache_read", x.CacheRead, rt.CacheRead}, {"cache_write", x.CacheWrite, rt.CacheWrite}} {
		if c.have > 0 && c.rate == 0 {
			miss = append(miss, c.n)
		}
	}
	if len(miss) > 0 {
		return no("missing_rate:" + strings.Join(miss, "+"))
	}
	x.Cost = ptr((float64(x.In)*rt.In + float64(x.Out)*rt.Out + float64(x.CacheRead)*rt.CacheRead + float64(x.CacheWrite)*rt.CacheWrite) / 1e6)
	if rt.CacheRead > 0 {
		x.saving = ptr(float64(x.CacheRead) * (rt.In - rt.CacheRead) / 1e6)
	}
	x.CostStatus = "priced"
	return x
}

func agg(rows []Row) Agg {
	a := Agg{Lines: len(rows)}
	ids, anon := map[string]bool{}, 0
	var sum float64
	for _, x := range rows {
		if x.ID == "" {
			anon++
		} else {
			ids[x.ID] = true
		}
		a.In, a.Out, a.CacheRead, a.CacheWrite = a.In+x.In, a.Out+x.Out, a.CacheRead+x.CacheRead, a.CacheWrite+x.CacheWrite
		if x.TQ == "unavailable" {
			a.Unmeasured++
		}
		if x.CostStatus == "priced" {
			a.Priced++
			sum += *x.Cost
		} else {
			a.Unpriced++
		}
	}
	a.Runs, a.Partial = len(ids)+anon, a.Unpriced > 0
	if a.Priced > 0 {
		a.Cost = ptr(sum)
	}
	return a
}

func group(rows []Row, names [2]string, key func(Row) (string, string)) []Group {
	m := map[[2]string][]Row{}
	for _, x := range rows {
		a, b := key(x)
		m[[2]string{a, b}] = append(m[[2]string{a, b}], x)
	}
	var out []Group
	for k, rs := range m {
		out = append(out, Group{Key1: k[0], Key2: k[1], Agg: agg(rs), names: names})
	}
	// group order is the order of the raw keys ("" model sorts first, as in 1.x)
	sort.Slice(out, func(i, j int) bool {
		ki, kj := out[i].Key1, out[j].Key1
		if ki == "(unknown)" {
			ki = ""
		}
		if kj == "(unknown)" {
			kj = ""
		}
		if ki != kj {
			return ki < kj
		}
		return out[i].Key2 < out[j].Key2
	})
	return out
}

func warnings(r *Result, tb *pricing.Table, psrc string, anon, agentRuns int, o Options) []string {
	var w []string
	if psrc == "none" {
		w = append(w, "no pricing table ("+psrc+"): cost is NOT computed, tokens only; add <project-dir>/pricing.json (see docs) or set TELEMETRY_PRICES")
	}
	if strings.HasPrefix(psrc, "unreadable") {
		w = append(w, "pricing table unreadable ("+psrc+"): cost is NOT computed")
	}
	if c := r.Pricing.Currency; c != nil && *c != "USD" {
		w = append(w, "pricing currency is "+*c+", not USD: the USD figures below are wrong for it")
	}
	if u := r.Totals.Unmeasured; u > 0 {
		w = append(w, fmt.Sprintf("%d run(s) reported no token usage: their zeros above are unmeasured, not counted usage", u))
	}
	if tb != nil {
		why := map[string][]Row{}
		for _, x := range r.Rows {
			if x.CostStatus == "unpriced" {
				why[x.CostReason] = append(why[x.CostReason], x)
			}
		}
		for _, k := range sortedKeys(why) {
			ms := map[string]bool{}
			for _, x := range why[k] {
				ms[orStr(x.Model, "(none)")] = true
			}
			w = append(w, fmt.Sprintf("%d run(s) not priced (%s): %s", len(why[k]), k, strings.Join(sortedKeys(ms), ", ")))
		}
	}
	has, orch := map[string]bool{}, map[string]bool{}
	for _, x := range r.Rows {
		if x.Session != "" && x.ID != "" {
			if x.Role == "orchestrator" {
				orch[x.Session] = true
			} else {
				has[x.Session] = true
			}
		}
	}
	for _, s := range sortedKeys(has) {
		if !orch[s] {
			w = append(w, "session "+s+" has agent runs but no orchestrator record (turn never ended or hook off): orchestrator usage is missing; try sdkit usage scan")
		}
	}
	nm := 0
	for _, x := range r.Rows {
		if x.Role != "orchestrator" && x.Model == "" && x.TQ != "unavailable" {
			nm++
		}
	}
	if nm > 0 {
		w = append(w, fmt.Sprintf("%d agent run(s) carry no model id: they cannot be priced", nm))
	}
	if o.Spawns != nil && o.Session == "" && *o.Spawns > agentRuns {
		w = append(w, fmt.Sprintf("%d subagent spawn(s) were counted for this task but only %d have a ledger record: %d execution(s) are missing", *o.Spawns, agentRuns, *o.Spawns-agentRuns))
	}
	if anon > 0 {
		w = append(w, fmt.Sprintf("%d legacy line(s) have no execution id: they cannot be deduplicated", anon))
	}
	return w
}

func usd(x *float64) string {
	if x == nil {
		return "-"
	}
	m := int64(math.Round(*x * 10000))
	return fmt.Sprintf("$%d.%04d", m/10000, m%10000)
}

func pct(x *float64) string {
	if x == nil {
		return "-"
	}
	m := int64(math.Round(*x * 1000))
	return fmt.Sprintf("%d.%d%%", m/10, m%10)
}

func table(rows [][]string) []string {
	w := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			w[i] = max(w[i], utf8.RuneCountInString(c))
		}
	}
	var out []string
	for _, r := range rows {
		var cells []string
		for i, c := range r {
			cells = append(cells, c+strings.Repeat(" ", w[i]-utf8.RuneCountInString(c)))
		}
		out = append(out, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
	return out
}

func (a Agg) cells() []string {
	c := "-"
	if a.Cost != nil {
		c = usd(a.Cost)
		if a.Partial {
			c += "*"
		}
	}
	return []string{fmt.Sprint(a.Runs), fmt.Sprint(a.In), fmt.Sprint(a.Out), fmt.Sprint(a.CacheRead), fmt.Sprint(a.CacheWrite), c}
}

// Text is the human report: usage per role, model and session, cache effectiveness, the API-equivalent estimate (never a bill) and every
// coverage warning. A figure that cannot be computed prints "-" or N/A text, never a number.
func (r *Result) Text() string {
	var b []string
	b = append(b, "usage: "+r.Scope, "")
	t := [][]string{{"role", "tier", "runs", "in", "out", "cache_read", "cache_write", "est_usd"}}
	for _, g := range r.ByRole {
		t = append(t, append([]string{g.Key1, g.Key2}, g.cells()...))
	}
	b = append(b, table(append(t, append([]string{"TOTAL", "-"}, r.Totals.cells()...)))...)
	t = [][]string{{"model", "provider", "runs", "in", "out", "cache_read", "cache_write", "est_usd"}}
	for _, g := range r.ByModel {
		t = append(t, append([]string{g.Key1, g.Key2}, g.cells()...))
	}
	b = append(b, "")
	b = append(b, table(t)...)
	if len(r.BySession) > 1 {
		t = [][]string{{"session", "origin", "runs", "in", "out", "cache_read", "cache_write", "est_usd"}}
		for _, g := range r.BySession {
			t = append(t, append([]string{g.Key1, g.Key2}, g.cells()...))
		}
		b = append(b, "")
		b = append(b, table(t)...)
	}
	c := fmt.Sprintf("cache hit rate: %s  (cache_read %d of %d input-side tokens = in + cache_read + cache_write)", pct(r.Cache.HitRate), r.Cache.Read, r.Cache.InputSide)
	if r.Cache.SavingUSD != nil && r.Cache.Read > 0 {
		c += "; saved about " + usd(r.Cache.SavingUSD) + " vs uncached input (API-equivalent)"
	}
	e := fmt.Sprintf("estimated cost (API-equivalent, tokens x %s): %s", r.Pricing.Source, usd(r.Totals.Cost))
	if r.Totals.Cost == nil {
		e += " - not computed"
	} else if r.Partial {
		e += fmt.Sprintf("  PARTIAL: %d of %d run(s) priced; the total is a lower bound", r.Totals.Priced, r.Totals.Runs)
	}
	b = append(b, "", c, e)
	if r.HostEstimate.USD != nil {
		b = append(b, fmt.Sprintf("host-reported estimate: %s over %d run(s) (%s; kept apart from the line above)", usd(r.HostEstimate.USD), r.HostEstimate.Runs, r.HostEstimate.Note))
	}
	b = append(b, "billed: unavailable - no source reports it; with a Claude Code subscription nothing is billed per token, so the figure above is only what the same tokens would cost at API rates")
	cov := fmt.Sprintf("coverage: %d execution(s), %d ledger line(s)", r.Coverage.Executions, r.Coverage.LedgerLines)
	if r.Coverage.Duplicates > 0 {
		cov += fmt.Sprintf(", %d duplicate/superseded line(s) collapsed", r.Coverage.Duplicates)
	}
	var os []string
	for _, x := range r.ByOrigin {
		os = append(os, fmt.Sprintf("%s %d", x.Origin, x.Runs))
	}
	b = append(b, cov+"; origin "+strings.Join(os, ", "))
	for _, w := range r.Warnings {
		b = append(b, "WARNING: "+w)
	}
	if r.Partial {
		b = append(b, "TOTAL IS PARTIAL: see the warnings; token counts and cost are lower bounds")
	} else {
		b = append(b, "totals complete for the data recorded (execution coverage cannot be proven for runs that never logged)")
	}
	return strings.Join(b, "\n") + "\n"
}
