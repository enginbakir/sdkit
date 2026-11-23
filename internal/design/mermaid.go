package design

import (
	"fmt"
	"strings"
)

func mm(s string) string {
	s = strings.NewReplacer("\"", "'", "\n", " ", "<", "&lt;", ">", "&gt;").Replace(s)
	return s
}

// Markdown renders the reader's twin of the BPMN file: a Mermaid flowchart (GitHub draws it in the pull request) and the table of
// screen candidates, one row per userTask. It is for people who will not open a BPMN tool. The model must have passed Check.
func Markdown(m *Model, version string) []byte {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	w("# %s\n\n", m.Title)
	w("Process flow for spec `%s` v%s, written to give the designer a first picture of the work before the real UI design. It is not a UI design. BPMN: `process.bpmn` (bpmn.io or Camunda Modeler); source: `process.json`.\n\n", m.Spec, version)
	w("```mermaid\nflowchart LR\n")
	names := map[string]string{}
	for _, ln := range m.Lanes {
		w("  subgraph lane_%s[\"%s\"]\n", ln.ID, mm(ln.Name))
		for _, n := range m.Nodes {
			if n.Lane != ln.ID {
				continue
			}
			id := "n_" + n.ID
			names[n.ID] = id
			t := mm(n.Name)
			switch n.Type {
			case Start:
				w("    %s([\"%s\"])\n", id, t)
			case End:
				w("    %s((\"%s\"))\n", id, t)
			case Xor:
				w("    %s{\"%s\"}\n", id, t)
			case And:
				w("    %s{\"%s +\"}\n", id, t)
			case ServiceTask:
				w("    %s[[\"%s\"]]\n", id, t)
			default:
				w("    %s[\"%s\"]\n", id, t)
			}
		}
		w("  end\n")
	}
	for _, f := range m.Flows {
		if f.Label != "" {
			w("  %s -->|\"%s\"| %s\n", names[f.From], mm(f.Label), names[f.To])
		} else {
			w("  %s --> %s\n", names[f.From], names[f.To])
		}
	}
	w("```\n\n## Screen candidates\n\n| Step | Actor | What the screen is for | AC |\n|---|---|---|---|\n")
	lane := map[string]string{}
	for _, ln := range m.Lanes {
		lane[ln.ID] = ln.Name
	}
	for _, n := range m.Nodes {
		if n.Type == UserTask {
			w("| %s | %s | %s | %s |\n", cell(n.Name), cell(lane[n.Lane]), cell(n.Doc), strings.Join(n.ACs, ", "))
		}
	}
	if len(m.NoUI) > 0 {
		w("\nAcceptance criteria with no visible result: %s.\n", strings.Join(m.NoUI, ", "))
	}
	return []byte(b.String())
}

func cell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(strings.TrimSpace(s))
}
