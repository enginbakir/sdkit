package design

import (
	"encoding/xml"
	"strings"
	"testing"
)

func okModel() *Model {
	return &Model{Spec: "login", Title: "Login", Lanes: []Lane{{"user", "User"}, {"sys", "System"}},
		Nodes: []Node{
			{ID: "s", Type: Start, Lane: "user", Name: "Opens the page"},
			{ID: "form", Type: UserTask, Lane: "user", Name: "Fills the form", Doc: "login form", ACs: []string{"AC-login-1"}},
			{ID: "chk", Type: ServiceTask, Lane: "sys", Name: "Checks credentials"},
			{ID: "gw", Type: Xor, Lane: "sys", Name: "Valid?"},
			{ID: "err", Type: UserTask, Lane: "user", Name: "Sees the error", Doc: "error above the form", ACs: []string{"AC-login-2"}},
			{ID: "e", Type: End, Lane: "user", Name: "Signed in"},
		},
		Flows: []Flow{{"s", "form", ""}, {"form", "chk", ""}, {"chk", "gw", ""}, {"gw", "e", "yes"}, {"gw", "err", "no"}, {"err", "form", ""}},
		NoUI:  []string{"AC-login-3"}}
}

var spec = Spec{ID: "login", Version: "0.1", Hash: "h", ACs: []string{"AC-login-1", "AC-login-2", "AC-login-3"}, Withdrawn: []string{"AC-login-4"}}

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		name string
		mut  func(*Model)
		want string // "" = no problem
	}{
		{"valid, with a loop back", func(m *Model) {}, ""},
		{"AC not accounted for", func(m *Model) { m.NoUI = nil }, "AC-login-3"},
		{"AC on a node and in noUI", func(m *Model) { m.NoUI = append(m.NoUI, "AC-login-1") }, "both on a node and in noUI"},
		{"AC the spec does not define", func(m *Model) { m.Nodes[1].ACs = []string{"AC-login-1", "AC-login-9"} }, "does not define"},
		{"withdrawn AC cited", func(m *Model) { m.NoUI = append(m.NoUI, "AC-login-4") }, "withdrew"},
		{"userTask without doc", func(m *Model) { m.Nodes[1].Doc = "" }, "no doc"},
		{"userTask without AC", func(m *Model) { m.Nodes[1].ACs = nil; m.NoUI = append(m.NoUI, "AC-login-1") }, "names no acceptance"},
		{"decision flow without label", func(m *Model) { m.Flows[3].Label = "" }, "without a label"},
		{"island not reached from a start", func(m *Model) {
			m.Nodes = append(m.Nodes, Node{ID: "p", Type: ServiceTask, Lane: "sys", Name: "p"}, Node{ID: "q", Type: ServiceTask, Lane: "sys", Name: "q"})
			m.Flows = append(m.Flows, Flow{"p", "q", ""}, Flow{"q", "p", ""})
		}, "cannot be reached"},
		{"flow to nowhere", func(m *Model) { m.Flows[0].To = "zz" }, "not defined"},
		{"end with outgoing flow", func(m *Model) { m.Flows = append(m.Flows, Flow{"e", "form", ""}) }, "end \"e\" has an outgoing"},
		{"id clashing with the output's own", func(m *Model) { m.Nodes[2].ID = "Lane_x"; m.Flows[1].To, m.Flows[2].From = "Lane_x", "Lane_x" }, "own elements"},
		{"wrong spec", func(m *Model) { m.Spec = "other" }, "expected"},
		{"unknown node type", func(m *Model) { m.Nodes[2].Type = "task" }, "has type"},
	} {
		m := okModel()
		c.mut(m)
		got := ""
		for _, p := range Check(m, spec) {
			got += p.String() + "\n"
		}
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q, got:\n%s", c.name, c.want, got)
		}
	}
}

// The layout must not put two nodes on one spot, and the BPMN must be well-formed XML with a shape per node.
func TestBPMNLayout(t *testing.T) {
	m := okModel()
	m.Nodes = append(m.Nodes, Node{ID: "e2", Type: End, Lane: "user", Name: "Gives up"})
	m.Flows = append(m.Flows, Flow{"err", "e2", ""})
	l := place(m)
	for a, ba := range l.nodes {
		for b, bb := range l.nodes {
			if a < b && ba.x < bb.x+bb.w && bb.x < ba.x+ba.w && ba.y < bb.y+bb.h && bb.y < ba.y+ba.h {
				t.Errorf("%s and %s overlap: %v %v", a, b, ba, bb)
			}
		}
	}
	x := BPMN(m)
	if err := xml.Unmarshal(x, new(struct{})); err != nil {
		t.Fatalf("not well-formed: %v", err)
	}
	if n := strings.Count(string(x), "<bpmndi:BPMNShape"); n != 1+len(m.Lanes)+len(m.Nodes) {
		t.Errorf("%d shapes", n)
	}
	if string(x) != string(BPMN(m)) {
		t.Error("output is not deterministic")
	}
}
