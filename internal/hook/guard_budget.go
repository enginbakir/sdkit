package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/route"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/usage"
)

func init() { Register(PreToolUse, []string{"Agent", "Task"}, "guard-budget", guardBudget) }

// guardBudget registers an independent role's spawn (so the verdict recorded when the agent stops can prove it came from a real run on this
// diff), then, with MAX_SPAWNS or TOKEN_BUDGET_TASK set, counts the spawn and checks the task's token budget. strict asks before the spawn that
// goes over, guided only warns, off does nothing. It never blocks because of its own problem.
func guardBudget(in *Input) Decision {
	role := roleOf(first(in.Str("tool_input", "subagent_type"), in.AgentType))
	e := sdd.New(in.Root)
	switch strings.TrimSuffix(role, "-deep") {
	case "verifier", "reviewer", "security-auditor":
		if _, err := os.Stat(filepath.Join(e.P.Dir(), ".sdd", "state.json")); err == nil {
			e.Err = nopWriter{}
			e.Out = nopWriter{}
			e.Spawn(role, []string{"--source", "hook"})
		}
	}
	if d, ok := guardRole(in, e); ok {
		return d
	}
	if setting(in, "MAX_SPAWNS") == "" && setting(in, "TOKEN_BUDGET_TASK") == "" {
		return Decision{}
	}
	mode := e.Mode()
	if mode == "off" {
		return Decision{}
	}
	slug := usage.CurrentSlug(e.P)
	if slug == "_misc" { // no open task and no work state: nothing to count against
		return Decision{}
	}
	s := in.Settings()
	if problems := usage.Check(e.P, s, slug); problems != "" {
		reason := fmt.Sprintf("Budget reached for task %s: %s. Confirm with the user before spawning another agent.", slug, problems)
		if mode == "guided" {
			return Decision{Message: reason}
		}
		return Decision{Kind: Ask, Reason: reason}
	}
	if b := usage.TokenBudget(e.P, s, slug); b.Level == usage.LevelWarn {
		return Decision{Message: b.Line(slug)}
	}
	return Decision{}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// guardRole is the AGENT_<ROLE>_TIER check (a role the project moved to another tier must be spawned with that tier's model) and, with
// ROUTING=on, the routing policy's preview (evidence to reuse, a BLOCK that stands, a duplicate, a model below the risk floor). The first
// problem found is the answer; anything it cannot decide lets the spawn through. strict refuses the tier mismatch and asks on a policy
// one, guided warns, off does nothing.
func guardRole(in *Input, e *sdd.Env) (Decision, bool) {
	raw := first(in.Str("tool_input", "subagent_type"), in.AgentType)
	if i := strings.LastIndex(raw, ":"); i >= 0 {
		raw = raw[i+1:]
	}
	if raw == "" {
		return Decision{}, false
	}
	text := ""
	for _, f := range []string{filepath.Join(e.P.Dir(), "roles", raw+".md"), ""} {
		if f == "" {
			if b, err := project.KitFile("core/roles/" + raw + ".md"); err == nil {
				text = string(b)
			}
			break
		}
		if b, err := os.ReadFile(f); err == nil {
			text = string(b)
			break
		}
	}
	if text == "" {
		return Decision{}, false
	}
	mode := e.Mode()
	say := func(deny bool, m string) (Decision, bool) {
		switch {
		case mode == "off":
			return Decision{}, false
		case mode == "guided":
			return Decision{Message: m}, true
		case deny:
			return Decision{Kind: Deny, Reason: m}, true
		}
		return Decision{Kind: Ask, Reason: m}, true
	}
	got := in.Str("tool_input", "model")
	get := setting
	if !strings.HasSuffix(raw, "-deep") {
		own := orStr(route.Frontmatter(text, "tier"), "standard")
		eff := own
		v := get(in, "AGENT_"+strings.ToUpper(strings.ReplaceAll(raw, "-", "_"))+"_TIER")
		if route.Rank(v) > 0 && (raw != "verifier" && raw != "reviewer" && raw != "security-auditor" || route.Rank(v) >= route.Rank(own)) {
			eff = v
		}
		if eff != own {
			gt := ""
			switch {
			case strings.Contains(got, "haiku"):
				gt = "light"
			case strings.Contains(got, "sonnet"):
				gt = "standard"
			case strings.Contains(got, "opus"):
				gt = "strong"
			}
			if want := tierModel(e.P, eff); want != "" && gt != eff {
				m := fmt.Sprintf("AGENT_%s_TIER=%s in sdkit.env: spawn %s with model \"%s\" (%s tier)", strings.ToUpper(strings.ReplaceAll(raw, "-", "_")), eff, raw, want, eff)
				if got != "" {
					m += fmt.Sprintf(", not \"%s\"", got)
				}
				if d, ok := say(true, m+"."); ok {
					return d, true
				}
			}
		}
	}
	if get(in, "ROUTING") != "on" {
		return Decision{}, false
	}
	r := route.New(in.Root)
	d, err := r.Decide(route.Query{Role: raw, Host: "claude"})
	if err != nil {
		return Decision{}, false
	}
	if d.Action == route.Reuse || d.Action == route.Stop && policyStop.MatchString(d.Reason) {
		return say(false, fmt.Sprintf("Routing policy (%s): %s. Spawn only if the user wants it anyway.", d.Reason, strings.Join(d.Notes, "; ")))
	}
	if got != "" && d.Tier != "" {
		reg, _ := r.Registry()
		for _, m := range reg {
			if m.Adapter == "claude" && m.Model != nil && *m.Model == got {
				if route.Rank(m.Tier) < route.Rank(d.Tier) {
					return say(false, fmt.Sprintf("Routing policy: model '%s' (%s) is below the %s tier this role needs at this risk. Use the %s model, or ask the user.", got, m.Tier, d.Tier, d.Tier))
				}
				break
			}
		}
	}
	return Decision{}, false
}

var policyStop = regexp.MustCompile(`^(EVIDENCE_|SECURITY_BLOCK$|VERIFICATION_FAILED$|DUPLICATE_)`)

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// tierModel is the Claude Code model alias of a tier: CLAUDE_MODEL_<TIER> in the kit's tiers.env, else the project's.
func tierModel(p project.Project, tier string) string {
	key := "CLAUDE_MODEL_" + strings.ToUpper(tier) + "="
	b, err := project.KitFile("core/tiers.env")
	if err != nil {
		b, _ = os.ReadFile(filepath.Join(p.Dir(), "tiers.env"))
	}
	v := ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key) {
			v = strings.TrimRight(strings.TrimPrefix(l, key), "\r")
		}
	}
	return v
}
