package hook

import "sort"

// Guard decides on one tool call. A panic counts as Allow.
type Guard func(*Input) Decision

type entry struct {
	event string
	tools map[string]bool // empty: every tool
	name  string
	fn    Guard
}

var registry []entry

// Register adds a guard; call it from an init function in guard_<name>.go.
// event is one of the event constants, tools the tool names it applies to
// ("Bash", "Write", ...; none means every tool), name the hook name used by
// `sdkit hook <name>`.
func Register(event string, tools []string, name string, fn Guard) {
	e := entry{event: event, tools: map[string]bool{}, name: name, fn: fn}
	for _, t := range tools {
		e.tools[t] = true
	}
	registry = append(registry, e)
}

// select returns the guards for target: an event name runs every guard of that event
// whose tool list matches; any other target runs the guard of that name.
func selectGuards(target, tool string) []entry {
	var out []entry
	for _, e := range registry {
		switch {
		case e.name == target:
			out = append(out, e)
		case e.event == target && (len(e.tools) == 0 || e.tools[tool]):
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}
