package usage

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/state"
)

// Levels of a token budget (the exit codes of `sdkit usage budget`).
const (
	LevelOK   = 0
	LevelWarn = 3 // from TOKEN_BUDGET_WARN_PCT (default 70)
	LevelOver = 4 // 100% and more
)

// BudgetStatus is the task's token use against TOKEN_BUDGET_TASK. Used counts in + out + cache writes; cache reads are cheap and left out.
// Set is false when there is no budget or no ledger yet (nothing to say: silent).
type BudgetStatus struct {
	Set         bool
	Used, Limit int
	Pct, Level  int
}

// Line is the one-line status ("token budget for <slug>: 8000 of 10000 used (80%)").
func (b BudgetStatus) Line(slug string) string {
	return fmt.Sprintf("token budget for %s: %d of %d used (%d%%)", slug, b.Used, b.Limit, b.Pct)
}

// TokenBudget reads the ledger lines as logged (they are already deduplicated at write time).
func TokenBudget(p project.Project, s *envfile.Settings, slug string) BudgetStatus {
	limit, err := strconv.Atoi(s.String("TOKEN_BUDGET_TASK"))
	if err != nil || limit <= 0 {
		return BudgetStatus{}
	}
	file := LedgerFile(p, slug)
	if _, err := os.Stat(file); err != nil {
		return BudgetStatus{}
	}
	b := BudgetStatus{Set: true, Limit: limit}
	for _, l := range ReadLines(file) {
		b.Used += int(l.In + l.Out + l.CacheWrite)
	}
	warn, err := strconv.Atoi(s.String("TOKEN_BUDGET_WARN_PCT"))
	if err != nil || warn < 0 {
		warn = 70
	}
	b.Pct = b.Used * 100 / limit
	switch {
	case b.Pct >= 100:
		b.Level = LevelOver
	case b.Pct >= warn:
		b.Level = LevelWarn
	}
	return b
}

// CountSpawn counts one subagent spawn for the task (state.sh spawn). problem is "" while within MAX_SPAWNS (empty = no limit).
func CountSpawn(p project.Project, s *envfile.Settings, slug string) (n int, problem string) {
	err := state.Update(p, slug, func(st *state.State) error { st.Spawns++; n = st.Spawns; return nil })
	if errors.Is(err, state.ErrMissing) {
		return 0, fmt.Sprintf("no state for '%s': run  sdkit state %s init", slug, slug)
	} else if err != nil {
		return 0, err.Error()
	}
	if max, e := strconv.Atoi(s.String("MAX_SPAWNS")); e == nil && max > 0 && n > max {
		return n, fmt.Sprintf("spawn %d is over MAX_SPAWNS=%d", n, max)
	}
	return n, ""
}

// Spawns is the count registered for the task, nil when the task has none.
func Spawns(p project.Project, slug string) *int {
	if st, err := state.Read(p, slug); err == nil {
		return &st.Spawns
	}
	return nil
}

// Check is the budget gate before a spawn: it counts the spawn and returns the limits already reached, joined ("" within the limits).
// A warning level (TOKEN_BUDGET_WARN_PCT) is not a stop; hooks read TokenBudget for it. The caller refuses BEFORE it starts the agent.
func Check(p project.Project, s *envfile.Settings, slug string) string {
	_, out := CountSpawn(p, s, slug)
	if b := TokenBudget(p, s, slug); b.Level == LevelOver {
		if out != "" {
			out += "; "
		}
		out += b.Line(slug)
	}
	return out
}
