package report

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Renderers: text and markdown are generated from the report each time, never stored.

func commas(x float64) string {
	r := math.Round(x)
	s := strconv.FormatFloat(math.Abs(r), 'f', 0, 64)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if r < 0 {
		s = "-" + s
	}
	return s
}

func cm(p *int) string {
	if p == nil {
		return "N/A"
	}
	return commas(float64(*p))
}

func usd(x *float64) string {
	if x == nil {
		return "N/A"
	}
	m := int64(math.Round(math.Abs(*x) * 10000))
	s := "$"
	if *x < 0 {
		s = "-$"
	}
	return fmt.Sprintf("%s%d.%04d", s, m/10000, m%10000)
}

func pctc(x *float64) string {
	if x == nil {
		return "N/A"
	}
	return num(*x) + "%"
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " ") }

func tbl(h []string, rows [][]string) []string {
	out := []string{"| " + strings.Join(h, " | ") + " |", "|" + strings.Repeat("---|", len(h))}
	for _, r := range rows {
		c := make([]string, len(r))
		for i, x := range r {
			c[i] = cell(x)
		}
		out = append(out, "| "+strings.Join(c, " | ")+" |")
	}
	return out
}

func orNA(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

func kvs(m map[string]int) string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	for i, x := range k {
		k[i] = fmt.Sprintf("%s=%d", x, m[x])
	}
	return strings.Join(k, " ")
}

func costTxt(c Cost) string {
	if c.State == "unavailable" {
		return "N/A (no pricing)"
	}
	s := usd(c.APIEquivalent)
	if c.State == "partial" {
		s += fmt.Sprintf(" (PARTIAL: %d run(s) unpriced, lower bound)", c.UnpricedRuns)
	}
	return s
}

func hitTxt(t Tokens) string {
	if t.CacheHitRate == nil {
		return ""
	}
	return " · cache hit " + num(math.Round(*t.CacheHitRate*1000)/10) + "%"
}

func tokensRow(t Tokens) string {
	if t.State == "unavailable" {
		return "N/A (nothing recorded)"
	}
	s := cm(t.Total)
	if t.State == "partial" {
		s += " (lower bound: some runs unmeasured)"
	}
	return s
}

// savView is what both report kinds give to the one savings sentence.
type savView struct {
	tokNA, costNA, regression bool
	tokText, costText, conf   string
	tokSaved, costSaved       float64
	tokRate, costRate         *float64
}

func (s savView) text() string {
	t := s.tokText
	if !s.tokNA {
		t = fmt.Sprintf("%s saved (%s, estimated, %s confidence)", commas(s.tokSaved), pctc(s.tokRate), s.conf)
	}
	c := s.costText
	if !s.costNA {
		c = fmt.Sprintf("%s (%s, estimated", usd(&s.costSaved), pctc(s.costRate))
		if s.regression {
			c += ", REGRESSION: SDKit cost more than the baseline"
		}
		c += ")"
	}
	return "tokens: " + t + " | cost: " + c
}

func (r *TaskReport) sav() savView {
	t, c := r.Savings.Tokens, r.Savings.Cost
	return savView{tokNA: t.State == "na", costNA: c.State == "na", regression: c.Regression, tokText: t.Text, costText: c.Text, conf: t.Confidence,
		tokSaved: t.SavedTokens, costSaved: c.SavedUSD, tokRate: t.RatePct, costRate: c.RatePct}
}

func (r *SessionReport) sav() savView {
	t, c := r.Savings.Tokens, r.Savings.Cost
	return savView{tokNA: t.State == "na", costNA: c.State == "na", tokText: t.Text, costText: c.Text, conf: "null", tokSaved: t.Saved, costSaved: c.Saved}
}

func (r *TaskReport) taskRows() [][]string {
	t := r.Task
	files, tests := "N/A", "not recorded"
	if t.FilesChanged != nil {
		files = fmt.Sprintf("%d (%s)", t.FilesChanged.N, t.FilesChanged.Method)
	}
	if t.Tests.State != "not recorded" {
		tests = fmt.Sprintf("%d passed / %d failed", *t.Tests.Passed, *t.Tests.Failed)
		if *t.Tests.RedExpected > 0 {
			tests += fmt.Sprintf(" (+%d expected red-phase failure(s))", *t.Tests.RedExpected)
		}
	}
	gate := t.QualityGates.State
	if n := t.QualityGates.Note; n != nil {
		gate += " (" + *n + ")"
	}
	st := strings.ToUpper(t.Status)
	if !t.Terminal {
		st += " (not terminal: live view)"
	}
	hosts := "N/A"
	if len(t.Hosts) > 0 {
		hosts = strings.Join(t.Hosts, ", ")
	}
	return [][]string{{"Task", t.Name}, {"Task ID", t.ID + "@" + orDash(t.Run)}, {"Status", st},
		{"Duration", fmt.Sprintf("%s (%s)", durText(t.Duration.S), t.Duration.Source)}, {"Workflow", fmt.Sprintf("%s (risk %s)", orNA(t.Profile, "N/A"), orNA(t.Risk, "N/A"))},
		{"Host", hosts}, {"Primary Model", orNA(t.PrimaryModel, "N/A")}, {"Files Changed", files}, {"Tests", tests}, {"Quality Gates", gate}}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (r *TaskReport) roleRows() [][]string {
	var out [][]string
	for _, x := range r.Roles {
		prov := ""
		if x.Provider != nil {
			prov = " @" + *x.Provider
		}
		cost := "N/A"
		if x.CostUSD != nil {
			cost = usd(x.CostUSD)
			if !x.CostComplete {
				cost += "*"
			}
		}
		out = append(out, []string{x.Role + " (" + x.Kind + ")", orNA(x.Model, "unknown") + prov, orNA(x.Purpose, "-"), strconv.Itoa(x.Runs), cm(x.Tokens), cost, x.ResultText})
	}
	return out
}

func (r *TaskReport) tokRows() [][]string {
	t, c, s := r.Tokens, r.Cost, r.Savings
	base, saved, rate, trate := "N/A — insufficient baseline data", "N/A", "N/A — insufficient baseline data", "N/A — insufficient baseline data"
	if s.Cost.State != "na" {
		base, saved, rate = usd(&s.Cost.BaselineUSD), usd(&s.Cost.SavedUSD), pctc(s.Cost.RatePct)
	}
	if s.Tokens.State != "na" {
		trate = fmt.Sprintf("%s (%s of %s fresh tokens)", pctc(s.Tokens.RatePct), commas(s.Tokens.SavedTokens), commas(s.Tokens.BaselineTokens))
	}
	return [][]string{{"Input Tokens", cm(t.In)}, {"Output Tokens", cm(t.Out)}, {"Cache Read Tokens", cm(t.CacheRead)}, {"Cache Write Tokens", cm(t.CacheWrite)},
		{"Total Recorded Tokens", tokensRow(t)}, {"Estimated API-Equivalent Cost", costTxt(c)}, {"Estimated Baseline Cost", base}, {"Estimated Savings", saved},
		{"Estimated Savings Rate (cost)", rate}, {"Estimated Savings Rate (tokens)", trate}}
}

func (r *TaskReport) markdown() []string {
	l := []string{"## SDKit Value Report — task " + r.Task.Name, "", "**" + r.Summary + "**", "", "### A. Task Overview", ""}
	l = append(l, tbl([]string{"Metric", "Value"}, r.taskRows())...)
	l = append(l, "", "### B. Agent & Role Breakdown", "")
	if len(r.Roles) == 0 {
		l = append(l, "No role execution was recorded (USAGE_LOG off, or nothing ran).")
	} else {
		l = append(l, tbl([]string{"Role / Agent", "Model", "Purpose", "Executions", "Tokens", "Cost", "Result"}, r.roleRows())...)
	}
	billed := r.Cost.Billed
	if billed == "" {
		billed = "null"
	}
	l = append(l, "", "_kind: orchestrator = the main session; subagent = a delegated agent; host-agent = a separate host process started by run-role.sh. Executions are distinct execution ids. `*` = some runs unpriced._", "",
		"### C. Token & Cost Breakdown", "")
	l = append(l, tbl([]string{"Metric", "Amount"}, r.tokRows())...)
	l = append(l, "", "_Cost is API-equivalent (tokens x your price table), not a bill. Billed: "+billed+"._", "", "### D. SDKit Optimization Impact", "")
	if len(r.Optimizations) == 0 {
		l = append(l, "No SDKit optimization was observed in the records of this task.")
	} else {
		var rows [][]string
		for _, o := range r.Optimizations {
			rows = append(rows, []string{o.Name, o.Evidence, o.Benefit, o.Class})
		}
		l = append(l, tbl([]string{"Optimization", "Evidence", "Benefit", "Type"}, rows)...)
	}
	return append(l, "", "**Savings** — "+r.sav().text(), "")
}

func (r *TaskReport) detailed() []string {
	l := r.markdown()
	var rows [][]string
	if t := r.Savings.Tokens; t.State == "na" {
		rows = append(rows, []string{"Token savings", "-", t.Why, "-", "-", "N/A"})
	} else {
		rows = append(rows, []string{"Token savings", t.BaselineSource, t.Method + ". " + t.Accounting, t.Coverage, t.Confidence, t.Kind})
	}
	if c := r.Savings.Cost; c.State == "na" {
		rows = append(rows, []string{"Cost savings", "-", c.Why, "-", "-", "N/A"})
	} else {
		var src []string
		for _, x := range c.Components {
			src = append(src, x.Source)
		}
		rows = append(rows, []string{"Cost savings", strings.Join(src, "; "), c.Method, c.Coverage, c.Confidence, c.Kind})
	}
	l = append(l, "### Savings method", "")
	l = append(l, tbl([]string{"Figure", "Baseline source", "Method", "Coverage", "Confidence", "Type"}, rows)...)
	l = append(l, "", "### Why each role ran", "")
	if len(r.Roles) == 0 {
		l = append(l, "-")
	} else {
		var wr [][]string
		for _, x := range r.Roles {
			why := "invoked directly (no routing record)"
			if len(x.Why) > 0 {
				why = strings.Join(x.Why, ", ")
			}
			wr = append(wr, []string{x.Role, why, strconv.Itoa(x.Runs), strconv.Itoa(x.UnmeasuredRun)})
		}
		l = append(l, tbl([]string{"Role", "Routing reasons", "Runs", "Unmeasured runs"}, wr)...)
	}
	v := r.Validation
	retries, fail := "N/A", kvs(v.Failures)
	if v.Retries != nil {
		retries = strconv.Itoa(*v.Retries)
	}
	if fail == "" {
		fail = "none"
	}
	l = append(l, "", "### Validation", "", "- tests: "+orNone(v.Tests, ", "), "- verification: "+orNone(v.Verification, " -> "), fmt.Sprintf("- retries: %s; failures: %s", retries, fail), "", "### Caveats", "")
	if len(r.Warnings) == 0 {
		l = append(l, "- none")
	}
	for _, w := range r.Warnings {
		l = append(l, "- "+w)
	}
	return append(l, "- cache saving (provider prompt caching, not SDKit): "+usd(r.Cost.CacheSaving))
}

func orNone(xs []string, sep string) string {
	if len(xs) == 0 {
		return "none recorded"
	}
	return strings.Join(xs, sep)
}

func (r *TaskReport) text() []string {
	t := r.Task
	files := "N/A"
	if t.FilesChanged != nil {
		files = strconv.Itoa(t.FilesChanged.N)
	}
	tests := "n/r"
	if t.Tests.State != "not recorded" {
		tests = fmt.Sprintf("%dp/%df", *t.Tests.Passed, *t.Tests.Failed)
	}
	hosts := "N/A"
	if len(t.Hosts) > 0 {
		hosts = strings.Join(t.Hosts, ",")
	}
	l := []string{"SDKit Value Report — " + r.Summary,
		fmt.Sprintf("  %s · workflow %s · host %s · files %s · tests %s · gates %s", durText(t.Duration.S), orNA(t.Profile, "N/A"), hosts, files, tests, t.QualityGates.State)}
	if len(r.Roles) > 0 {
		l = append(l, "  roles:")
		for _, x := range r.Roles {
			c := "N/A"
			if x.CostUSD != nil {
				c = usd(x.CostUSD)
			}
			l = append(l, fmt.Sprintf("    %s [%s] %s x%d  %s tok  %s", x.Role, x.Kind, orNA(x.Model, "?"), x.Runs, cm(x.Tokens), c))
		}
	} else {
		l = append(l, "  roles: none recorded")
	}
	k := r.Tokens
	opts := "none observed"
	if len(r.Optimizations) > 0 {
		var o []string
		for _, x := range r.Optimizations {
			o = append(o, x.Name+" ["+x.Class+"]")
		}
		opts = strings.Join(o, ", ")
	}
	return append(l, fmt.Sprintf("  usage: %s tokens (in %s out %s cache_read %s cache_write %s)%s · est. API-equivalent cost %s", tokensRow(k), cm(k.In), cm(k.Out), cm(k.CacheRead), cm(k.CacheWrite), hitTxt(k), costTxt(r.Cost)),
		"  savings: "+r.sav().text(), "  optimizations: "+opts,
		fmt.Sprintf("  validation: tests %s · verification %s", orNone(r.Validation.Tests, ", "), orNA(r.Validation.Final, "not recorded")),
		fmt.Sprintf("  details: sdkit report task %s --detailed   (--json / --markdown to export)", t.ID))
}

func nul(p *int) string {
	if p == nil {
		return "null"
	}
	return strconv.Itoa(*p)
}

func (r *SessionReport) markdown() []string {
	var wr [][]string
	for _, x := range r.Roles {
		prov := ""
		if x.Provider != nil {
			prov = " @" + *x.Provider
		}
		wr = append(wr, []string{x.Role + " (" + x.Kind + ")", orNA(x.Model, "unknown") + prov, strconv.Itoa(x.Runs), cm(x.Tokens), usd(x.CostUSD)})
	}
	var tr [][]string
	for _, t := range r.Tasks.List {
		tr = append(tr, []string{t.Task, t.Status, cm(t.TokensFresh), usd(t.CostUSD)})
	}
	gates := kvs(r.Validation.Gates)
	if gates == "" {
		gates = "n/a"
	}
	t := r.Tasks
	rows := [][]string{{"Session", r.Session.ID}, {"State", r.Session.State + " — " + r.Session.Note},
		{"Tasks", fmt.Sprintf("%d (completed %d, partial %d, failed/aborted %d, blocked %d)", t.Total, t.Completed, t.Partial, t.Failed, t.Blocked)},
		{"Role executions", fmt.Sprintf("%d (unmeasured %d)", r.Executions.Total, r.Executions.Unmeasured)}, {"Total tokens", tokensRow(r.Tokens)}, {"Est. API-equivalent cost", costTxt(r.Cost)},
		{"Evidence reuse", fmt.Sprintf("%d execution(s) avoided", r.EvidenceReuse)}, {"Skipped executions", strconv.Itoa(r.Skipped)},
		{"Validation", fmt.Sprintf("tests %s passed / %s failed; gates %s", nul(r.Validation.TestsPassed), nul(r.Validation.TestsFailed), gates)}, {"Savings", r.sav().text()}}
	l := []string{"## SDKit Session Summary — " + r.Session.ID, "", "**" + r.Summary + "**", ""}
	l = append(l, tbl([]string{"Metric", "Value"}, rows)...)
	l = append(l, "", "### Agent & role participation", "")
	if len(r.Roles) == 0 {
		l = append(l, "No role execution recorded.")
	} else {
		l = append(l, tbl([]string{"Role / Agent", "Model", "Executions", "Tokens", "Cost"}, wr)...)
	}
	l = append(l, "", "### Tasks", "")
	if len(r.Tasks.List) == 0 {
		l = append(l, "No stored task report belongs to this session.")
	} else {
		l = append(l, tbl([]string{"Task", "Status", "Fresh tokens", "Cost"}, tr)...)
	}
	rt := "N/A — " + r.Savings.Routing.Reason
	if r.Savings.Routing.State == "ok" {
		d := r.Savings.Routing.DeltaUSD
		rt = usd(&d) + " vs baseline " + r.Savings.Routing.BaselineModel + " (same-token counterfactual, estimated)"
	}
	cnt := kvs(r.Savings.Avoided.Counted)
	if cnt == "" {
		cnt = "none"
	}
	l = append(l, "", "### Savings", "", "- routing: "+rt, "- avoided executions: "+cnt+"; "+r.Savings.Avoided.Excluded, "", "### Caveats", "")
	if len(r.Warnings) == 0 {
		l = append(l, "- none")
	}
	for _, w := range r.Warnings {
		l = append(l, "- "+w)
	}
	return l
}

func (r *SessionReport) text() []string {
	t := r.Tasks
	var names []string
	for _, x := range r.Roles {
		names = append(names, x.Role)
	}
	return []string{"SDKit Session Summary — " + r.Summary,
		fmt.Sprintf("  tasks %d: completed %d, partial %d, failed/aborted %d, blocked %d", t.Total, t.Completed, t.Partial, t.Failed, t.Blocked),
		fmt.Sprintf("  executions %d across %d role(s) · tokens %s%s · est. API-equivalent cost %s", r.Executions.Total, len(uniq(names)), tokensRow(r.Tokens), hitTxt(r.Tokens), costTxt(r.Cost)),
		fmt.Sprintf("  evidence reuse %d · skipped %d · tests %sp/%sf", r.EvidenceReuse, r.Skipped, nul(r.Validation.TestsPassed), nul(r.Validation.TestsFailed)),
		"  savings: " + r.sav().text(), "  details: sdkit report session " + r.Session.ID + " --detailed"}
}
