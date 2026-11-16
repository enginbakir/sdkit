package review

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/audit"
	"github.com/wecii/sdkit/internal/store"
)

// baseID is the spec id of a review id: the draft review "<id>-draft" and the output review "<id>" share one settled list.
func baseID(id string) string { return strings.TrimSuffix(id, "-draft") }

func (c Ctx) decisionsFile(id string) string {
	return filepath.Join(c.Root, c.KitDir, "work", "decisions", baseID(id)+".md")
}

// Settled reads the settled entries of a spec: the working file, then the published <SPECS_DIR>/<id>/decisions.md.
func (c Ctx) Settled(id string) []string {
	var out []string
	for _, f := range []string{c.decisionsFile(id), filepath.Join(c.Root, c.SpecsDir, baseID(id), "decisions.md")} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "- [S") {
				out = append(out, l)
			}
		}
		if len(out) > 0 {
			break // the working file is the live one; the published copy only seeds a fresh clone
		}
	}
	return out
}

// Settle records a topic a person has decided, so reviewers are told not to reopen it. It needs a person and a reason, like accept:
// an orchestrator never settles a finding away on its own. It does not touch any finding or the PASS rules.
func (c Ctx) Settle(id, topic, why, who string) (string, error) {
	if strings.TrimSpace(who) == "" {
		who = c.JiraUser
	}
	if strings.TrimSpace(who) == "" || strings.TrimSpace(why) == "" || strings.TrimSpace(topic) == "" {
		return "", fail(2, "settle needs a topic, --why \"<the decision and its reason>\" and a person: --who or JIRA_USER in sdkit.local.env (only a person's decision is settled)")
	}
	clean := func(s string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("|", "/", "\n", " ").Replace(s)), " ")
	}
	f := c.decisionsFile(id)
	unlock, err := store.Lock(f)
	if err != nil {
		return "", fail(2, "cannot lock %s", f)
	}
	defer unlock()
	n := len(c.Settled(id)) + 1
	if _, err := os.Stat(f); err != nil {
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		_ = os.WriteFile(f, []byte("# Settled decisions of "+baseID(id)+"\n\nA person decided each topic below. Reviewers do not reopen them; a finding that does is rejected with this entry as the reason.\n\n"), 0o644)
	}
	line := fmt.Sprintf("- [S%d] %s | %s | %s | %s", n, clean(topic), clean(why), clean(who), now()[:10])
	if err := store.AppendLine(f, line); err != nil {
		return "", fail(2, "cannot write %s", f)
	}
	return fmt.Sprintf("review: settled S%d for '%s' by %s (%s). It goes into the reviewers' prompt; copy it to %s/%s/decisions.md at the propose step.", n, baseID(id), who, f, c.SpecsDir, baseID(id)), nil
}

// SettledPrompt is the block to paste into every reviewer's prompt ("" when nothing is settled).
func (c Ctx) SettledPrompt(id string) string {
	l := c.Settled(id)
	if len(l) == 0 {
		return ""
	}
	return "Settled by a person; do not raise these again, and do not suggest changing them:\n" + strings.Join(l, "\n") + "\n"
}

// RaiseCap lets a person raise the round cap of one review without editing sdkit.env; it is audited.
func (c Ctx) RaiseCap(id string, to int, why, who string) (string, error) {
	if strings.TrimSpace(who) == "" {
		who = c.JiraUser
	}
	if strings.TrimSpace(who) == "" || strings.TrimSpace(why) == "" {
		return "", fail(2, "raise-cap needs --why \"<reason>\" and a person: --who or JIRA_USER in sdkit.local.env. Run it only after the user has said yes to the new number.")
	}
	var msg string
	err := store.Update(c.stateFile(id), func(old []byte) ([]byte, error) {
		var st State
		if len(old) == 0 {
			return nil, fail(2, "no review state for '%s': run review begin first", id)
		}
		_ = jsonUnmarshal(old, &st)
		cur := max(c.MaxRounds, st.CapOverride)
		if to <= cur {
			return nil, fail(2, "--to %d is not above the current cap %d", to, cur)
		}
		st.CapOverride = to
		if err := audit.Log(audit.Path(filepath.Join(c.Root, c.KitDir)), "review.raisecap", []string{"id=" + id, fmt.Sprintf("from=%d", cur), fmt.Sprintf("to=%d", to), "who=" + who, "why=" + why}); err != nil {
			return nil, fail(2, "cannot write the audit record, so the cap stays %d", cur)
		}
		msg = fmt.Sprintf("review: cap of '%s' raised from %d to %d by %s (audited)", id, cur, to, who)
		return []byte(compact(st) + "\n"), nil
	})
	return msg, err
}

// Changed lists the files that differ from the last recorded evidence (all of them when there is none): the only ones a new round needs to review.
func (c Ctx) Changed(id string) ([]string, int, error) {
	t, err := c.target(id, "")
	if err != nil {
		return nil, 0, err
	}
	now, err := FileHashes(t)
	if err != nil {
		return nil, 0, err
	}
	st, _ := readState(c.stateFile(id))
	var base map[string]string
	if st.Evidence != nil {
		base = st.Evidence.Files
	}
	var out []string
	same := 0
	for f, h := range now {
		switch bh, ok := base[f]; {
		case !ok:
			out = append(out, "new "+f)
		case bh != h:
			out = append(out, "changed "+f)
		default:
			same++
		}
	}
	for f := range base {
		if _, ok := now[f]; !ok {
			out = append(out, "removed "+f)
		}
	}
	sort.Strings(out)
	return out, same, nil
}

// Status is the one-screen picture of a review: rounds, cap, the trend, and whether it has converged.
func (c Ctx) Status(id string) string {
	st, ok := readState(c.stateFile(id))
	if !ok {
		return fmt.Sprintf("review '%s': no round yet", id)
	}
	capN := max(c.MaxRounds, st.CapOverride)
	var b strings.Builder
	fmt.Fprintf(&b, "review '%s': round %d of %d", id, st.Round, capN)
	if st.CapOverride > c.MaxRounds {
		fmt.Fprintf(&b, " (raised from %d)", c.MaxRounds)
	}
	r := c.Check(id)
	fmt.Fprintf(&b, "; now: %s\n", r.State)
	roles := 0
	b.WriteString("round  set    verdict   BLOCKER MAJOR MINOR  roles  ACs\n")
	for _, n := range st.History {
		roles += n.Roles
		fmt.Fprintf(&b, "%-6d %-6s %-9s %-7d %-5d %-5d  %-5d  %d\n", n.Round, n.Set, n.Verdict, n.Blocker, n.Major, n.Minor, n.Roles, n.ACs)
	}
	fmt.Fprintf(&b, "reviewer runs recorded: %d (record --roles N). Token and cost: sdkit report session.\n", roles)
	if n := len(c.Settled(id)); n > 0 {
		fmt.Fprintf(&b, "settled topics: %d\n", n)
	}
	if Converged(st.History) {
		b.WriteString("CONVERGED: the MAJOR count did not rise for two rounds. Stop opening rounds: show the remaining MAJOR/BLOCKER findings to the analyst in one screen to accept or fix (triage.md, \"At the round cap\"); raising the cap is a person's call and costs about one more round of the figures above.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Converged: the last three rounds' BLOCKER+MAJOR counts never went up (so the review is not getting worse, only sampling).
func Converged(h []Note) bool {
	if len(h) < 3 {
		return false
	}
	h = h[len(h)-3:]
	n := func(x Note) int { return x.Blocker + x.Major }
	return n(h[1]) <= n(h[0]) && n(h[2]) <= n(h[1]) && n(h[2]) > 0
}
