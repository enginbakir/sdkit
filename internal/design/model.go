// Package design turns the process model a flow-modeler writes from a published spec into a BPMN 2.0 file, a Mermaid twin for the
// pull request and a metadata file, and checks the model against the spec. The model is JSON with a fixed order, so the outputs are
// byte-for-byte reproducible and safe to commit. The package takes its inputs as parameters; reading the spec and git is the caller's.
package design

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Model is the process of one spec: who acts (lanes), what happens (nodes) and in which order (flows).
type Model struct {
	Spec  string   `json:"spec"`
	Title string   `json:"title"`
	Lanes []Lane   `json:"lanes"`
	Nodes []Node   `json:"nodes"`
	Flows []Flow   `json:"flows"`
	NoUI  []string `json:"noUI,omitempty"` // acceptance criteria with no visible result: they need no node
}

// Lane is an actor (a user role, the system, an external party).
type Lane struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Node types.
const (
	Start       = "start"
	End         = "end"
	UserTask    = "userTask"
	ServiceTask = "serviceTask"
	Xor         = "xor"
	And         = "and"
)

// Node is a step. A userTask is a screen candidate: Doc says what the screen is for, what it shows and what the person can do.
type Node struct {
	ID   string   `json:"id"`
	Type string   `json:"type"`
	Lane string   `json:"lane"`
	Name string   `json:"name"`
	Doc  string   `json:"doc,omitempty"`
	ACs  []string `json:"acs,omitempty"`
}

// Flow is a sequence flow; Label names the condition on a flow that leaves a gateway.
type Flow struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Spec is what the model is checked against: the published spec's identity and its acceptance criteria.
type Spec struct {
	ID, Version, Hash string
	ACs               []string // not withdrawn
	Withdrawn         []string
}

// Problem is one finding of Check; Level is "error" or "warn".
type Problem struct{ Level, Msg string }

func (p Problem) String() string { return p.Level + ": " + p.Msg }

// Errors counts the error-level problems.
func Errors(ps []Problem) int {
	n := 0
	for _, p := range ps {
		if p.Level == "error" {
			n++
		}
	}
	return n
}

// Load reads and decodes a model file; unknown fields are an error so a misspelt key is not silently dropped.
func Load(path string) (*Model, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var m Model
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return nil, nil, fmt.Errorf("%s: %v", path, err)
	}
	return &m, b, nil
}

var (
	idRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	reserved = []string{"Lane_", "Flow_", "Participant_", "Process_", "Collaboration_", "Diagram_", "Plane_", "Definitions_"}
)

func validType(t string) bool {
	switch t {
	case Start, End, UserTask, ServiceTask, Xor, And:
		return true
	}
	return false
}

// Check judges the model: well-formed, connected, every userTask traced to the spec, every active AC accounted for.
func Check(m *Model, sp Spec) []Problem {
	var ps []Problem
	bad := func(f string, a ...any) { ps = append(ps, Problem{"error", fmt.Sprintf(f, a...)}) }
	warn := func(f string, a ...any) { ps = append(ps, Problem{"warn", fmt.Sprintf(f, a...)}) }

	if m.Spec != sp.ID {
		bad("spec is %q, expected %q", m.Spec, sp.ID)
	}
	if strings.TrimSpace(m.Title) == "" {
		bad("title is empty")
	}
	lanes := map[string]bool{}
	if len(m.Lanes) == 0 {
		bad("no lanes: name at least one actor")
	}
	for _, l := range m.Lanes {
		switch {
		case !idRe.MatchString(l.ID):
			bad("lane id %q must be letters, digits and _, starting with a letter", l.ID)
		case lanes[l.ID]:
			bad("lane %q is defined twice", l.ID)
		case strings.TrimSpace(l.Name) == "":
			bad("lane %q has no name", l.ID)
		}
		lanes[l.ID] = true
	}
	nodes := map[string]*Node{}
	starts, ends := 0, 0
	for i := range m.Nodes {
		n := &m.Nodes[i]
		switch {
		case !idRe.MatchString(n.ID):
			bad("node id %q must be letters, digits and _, starting with a letter", n.ID)
			continue
		case nodes[n.ID] != nil:
			bad("node %q is defined twice", n.ID)
			continue
		}
		nodes[n.ID] = n
		for _, r := range reserved {
			if strings.HasPrefix(n.ID, r) {
				bad("node id %q starts with %q, which the BPMN output uses for its own elements", n.ID, r)
			}
		}
		if strings.HasSuffix(n.ID, "_di") {
			bad("node id %q ends with _di, which the BPMN output uses for its own elements", n.ID)
		}
		if !validType(n.Type) {
			bad("node %q has type %q: use start, end, userTask, serviceTask, xor or and", n.ID, n.Type)
		}
		if !lanes[n.Lane] {
			bad("node %q is in lane %q, which is not defined", n.ID, n.Lane)
		}
		if strings.TrimSpace(n.Name) == "" {
			bad("node %q has no name", n.ID)
		}
		if n.Type == Start {
			starts++
		}
		if n.Type == End {
			ends++
		}
		if n.Type == UserTask {
			if strings.TrimSpace(n.Doc) == "" {
				bad("userTask %q has no doc: say what the screen is for, what it shows and what the person can do", n.ID)
			}
			if len(n.ACs) == 0 {
				bad("userTask %q names no acceptance criterion", n.ID)
			}
		}
	}
	if starts == 0 {
		bad("no start node")
	}
	if ends == 0 {
		bad("no end node")
	}
	in, out := map[string][]Flow{}, map[string][]Flow{}
	seen := map[string]bool{}
	for _, f := range m.Flows {
		k := f.From + ">" + f.To + ">" + f.Label
		switch {
		case nodes[f.From] == nil || nodes[f.To] == nil:
			bad("flow %s -> %s uses a node that is not defined", f.From, f.To)
			continue
		case f.From == f.To:
			bad("flow %s -> %s loops on itself", f.From, f.To)
			continue
		case seen[k]:
			bad("flow %s -> %s is listed twice", f.From, f.To)
			continue
		}
		seen[k] = true
		in[f.To] = append(in[f.To], f)
		out[f.From] = append(out[f.From], f)
	}
	for _, n := range m.Nodes {
		if nodes[n.ID] == nil || !validType(n.Type) {
			continue
		}
		if n.Type == Start && len(in[n.ID]) > 0 {
			bad("start %q has an incoming flow", n.ID)
		}
		if n.Type != Start && len(in[n.ID]) == 0 {
			bad("node %q has no incoming flow", n.ID)
		}
		if n.Type == End && len(out[n.ID]) > 0 {
			bad("end %q has an outgoing flow", n.ID)
		}
		if n.Type != End && len(out[n.ID]) == 0 {
			bad("node %q has no outgoing flow", n.ID)
		}
		if n.Type == Xor && len(out[n.ID]) > 1 {
			for _, f := range out[n.ID] {
				if strings.TrimSpace(f.Label) == "" {
					bad("flow %s -> %s leaves the decision %q without a label", f.From, f.To, n.ID)
				}
			}
		}
	}
	// every node must be reachable from a start
	reach := map[string]bool{}
	var q []string
	for _, n := range m.Nodes {
		if n.Type == Start {
			reach[n.ID] = true
			q = append(q, n.ID)
		}
	}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		for _, f := range out[c] {
			if !reach[f.To] {
				reach[f.To] = true
				q = append(q, f.To)
			}
		}
	}
	for _, n := range m.Nodes {
		if nodes[n.ID] != nil && !reach[n.ID] && n.Type != Start {
			bad("node %q cannot be reached from a start", n.ID)
		}
	}
	// traceability: every cited AC exists and is active; every active AC is in a node or in noUI, not both
	active, wd := set(sp.ACs), set(sp.Withdrawn)
	used := map[string]bool{}
	cite := func(where, id string) {
		switch {
		case active[id]:
		case wd[id]:
			bad("%s cites %s, which the spec withdrew", where, id)
		default:
			bad("%s cites %s, which the spec does not define", where, id)
		}
	}
	for _, n := range m.Nodes {
		for _, a := range n.ACs {
			cite("node "+n.ID, a)
			used[a] = true
		}
	}
	noUI := map[string]bool{}
	for _, a := range m.NoUI {
		cite("noUI", a)
		if used[a] {
			bad("%s is both on a node and in noUI", a)
		}
		noUI[a] = true
	}
	var missing []string
	for _, a := range sp.ACs {
		if !used[a] && !noUI[a] {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		bad("acceptance criteria not accounted for (put each on the node that shows its result, or in noUI): %s", strings.Join(missing, ", "))
	}
	if len(ps) == 0 && len(m.Nodes) > 40 {
		warn("%d nodes: a process this long is hard to read; consider splitting the feature", len(m.Nodes))
	}
	return ps
}

func set(l []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range l {
		m[s] = true
	}
	return m
}
