package design

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Layout, in BPMN diagram units. Columns are process steps from left to right, lanes are bands from top to bottom.
const (
	poolX, poolY = 160, 80
	labelBand    = 30  // the vertical title of the pool and of a lane
	padLeft      = 40  // before the first column
	colW         = 180 // one process step
	rowH         = 100 // one node stacked in a lane and column
	lanePad      = 20
)

type box struct{ x, y, w, h int }

func (b box) cx() int { return b.x + b.w/2 }
func (b box) cy() int { return b.y + b.h/2 }

func size(t string) (int, int) {
	switch t {
	case Start, End:
		return 36, 36
	case Xor, And:
		return 50, 50
	}
	return 100, 80
}

type layout struct {
	lanes map[string]box
	nodes map[string]box
	pool  box
	layer map[string]int
}

// place gives every node a column (its longest path from a start, loops ignored) and a row in its lane.
func place(m *Model) *layout {
	idx := map[string]int{}
	for i, n := range m.Nodes {
		idx[n.ID] = i
	}
	out := map[string][]string{}
	for _, f := range m.Flows {
		out[f.From] = append(out[f.From], f.To)
	}
	// back edges: an edge to a node still on the DFS stack
	back := map[[2]string]bool{}
	state := map[string]int{}
	var dfs func(string)
	dfs = func(n string) {
		state[n] = 1
		for _, t := range out[n] {
			switch state[t] {
			case 0:
				dfs(t)
			case 1:
				back[[2]string{n, t}] = true
			}
		}
		state[n] = 2
	}
	for _, n := range m.Nodes {
		if n.Type == Start && state[n.ID] == 0 {
			dfs(n.ID)
		}
	}
	layer := map[string]int{}
	for range m.Nodes { // longest path by relaxation; at most len(Nodes) passes settle a DAG
		changed := false
		for _, f := range m.Flows {
			if back[[2]string{f.From, f.To}] {
				continue
			}
			if layer[f.To] < layer[f.From]+1 {
				layer[f.To] = layer[f.From] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	maxLayer := 0
	rows := map[string]map[int]int{} // lane -> layer -> next row
	row := map[string]int{}
	laneRows := map[string]int{}
	for _, n := range m.Nodes {
		if layer[n.ID] > maxLayer {
			maxLayer = layer[n.ID]
		}
		if rows[n.Lane] == nil {
			rows[n.Lane] = map[int]int{}
		}
		row[n.ID] = rows[n.Lane][layer[n.ID]]
		rows[n.Lane][layer[n.ID]]++
		if rows[n.Lane][layer[n.ID]] > laneRows[n.Lane] {
			laneRows[n.Lane] = rows[n.Lane][layer[n.ID]]
		}
	}
	l := &layout{lanes: map[string]box{}, nodes: map[string]box{}, layer: layer}
	x0 := poolX + 2*labelBand
	width := 2*labelBand + padLeft + (maxLayer+1)*colW
	y := poolY
	for _, ln := range m.Lanes {
		r := laneRows[ln.ID]
		if r < 1 {
			r = 1
		}
		h := r*rowH + 2*lanePad
		l.lanes[ln.ID] = box{poolX + labelBand, y, width - labelBand, h}
		y += h
	}
	l.pool = box{poolX, poolY, width, y - poolY}
	for _, n := range m.Nodes {
		w, h := size(n.Type)
		lb := l.lanes[n.Lane]
		cx := x0 + padLeft + layer[n.ID]*colW + colW/2 - 40
		cy := lb.y + lanePad + row[n.ID]*rowH + rowH/2
		l.nodes[n.ID] = box{cx - w/2, cy - h/2, w, h}
	}
	return l
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func tag(t string) string {
	switch t {
	case Start:
		return "startEvent"
	case End:
		return "endEvent"
	case UserTask:
		return "userTask"
	case ServiceTask:
		return "serviceTask"
	case Xor:
		return "exclusiveGateway"
	}
	return "parallelGateway"
}

func docOf(n Node) string {
	d := strings.TrimSpace(n.Doc)
	if len(n.ACs) > 0 {
		if d != "" {
			d += "\n"
		}
		d += "AC: " + strings.Join(n.ACs, ", ")
	}
	return d
}

// BPMN renders the model as BPMN 2.0 XML with diagram interchange, so bpmn.io and Camunda Modeler draw it without further work.
// The model must have passed Check.
func BPMN(m *Model) []byte {
	l := place(m)
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	in, out := map[string][]string{}, map[string][]string{}
	fid := func(i int) string { return fmt.Sprintf("Flow_%d", i+1) }
	for i, f := range m.Flows {
		out[f.From] = append(out[f.From], fid(i))
		in[f.To] = append(in[f.To], fid(i))
	}
	w("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	w("<bpmn:definitions xmlns:bpmn=\"http://www.omg.org/spec/BPMN/20100524/MODEL\" xmlns:bpmndi=\"http://www.omg.org/spec/BPMN/20100524/DI\" xmlns:dc=\"http://www.omg.org/spec/DD/20100524/DC\" xmlns:di=\"http://www.omg.org/spec/DD/20100524/DI\" id=\"Definitions_%s\" targetNamespace=\"http://sdkit.dev/design\">\n", esc(m.Spec))
	w("  <bpmn:collaboration id=\"Collaboration_1\">\n    <bpmn:participant id=\"Participant_1\" name=\"%s\" processRef=\"Process_1\" />\n  </bpmn:collaboration>\n", esc(m.Title))
	w("  <bpmn:process id=\"Process_1\" isExecutable=\"false\">\n    <bpmn:laneSet id=\"LaneSet_1\">\n")
	for _, ln := range m.Lanes {
		w("      <bpmn:lane id=\"Lane_%s\" name=\"%s\">\n", ln.ID, esc(ln.Name))
		for _, n := range m.Nodes {
			if n.Lane == ln.ID {
				w("        <bpmn:flowNodeRef>%s</bpmn:flowNodeRef>\n", n.ID)
			}
		}
		w("      </bpmn:lane>\n")
	}
	w("    </bpmn:laneSet>\n")
	for _, n := range m.Nodes {
		w("    <bpmn:%s id=\"%s\" name=\"%s\">\n", tag(n.Type), n.ID, esc(n.Name))
		if d := docOf(n); d != "" {
			w("      <bpmn:documentation>%s</bpmn:documentation>\n", esc(d))
		}
		for _, f := range in[n.ID] {
			w("      <bpmn:incoming>%s</bpmn:incoming>\n", f)
		}
		for _, f := range out[n.ID] {
			w("      <bpmn:outgoing>%s</bpmn:outgoing>\n", f)
		}
		w("    </bpmn:%s>\n", tag(n.Type))
	}
	for i, f := range m.Flows {
		if f.Label != "" {
			w("    <bpmn:sequenceFlow id=\"%s\" name=\"%s\" sourceRef=\"%s\" targetRef=\"%s\" />\n", fid(i), esc(f.Label), f.From, f.To)
		} else {
			w("    <bpmn:sequenceFlow id=\"%s\" sourceRef=\"%s\" targetRef=\"%s\" />\n", fid(i), f.From, f.To)
		}
	}
	w("  </bpmn:process>\n")
	w("  <bpmndi:BPMNDiagram id=\"Diagram_1\">\n    <bpmndi:BPMNPlane id=\"Plane_1\" bpmnElement=\"Collaboration_1\">\n")
	shape := func(id, el string, bx box, extra string) {
		w("      <bpmndi:BPMNShape id=\"%s_di\" bpmnElement=\"%s\"%s>\n        <dc:Bounds x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" />\n      </bpmndi:BPMNShape>\n", id, el, extra, bx.x, bx.y, bx.w, bx.h)
	}
	shape("Participant_1", "Participant_1", l.pool, " isHorizontal=\"true\"")
	for _, ln := range m.Lanes {
		shape("Lane_"+ln.ID, "Lane_"+ln.ID, l.lanes[ln.ID], " isHorizontal=\"true\"")
	}
	for _, n := range m.Nodes {
		extra := ""
		if n.Type == Xor {
			extra = " isMarkerVisible=\"true\""
		}
		shape(n.ID, n.ID, l.nodes[n.ID], extra)
	}
	for i, f := range m.Flows {
		s, t := l.nodes[f.From], l.nodes[f.To]
		var pts [][2]int
		if l.layer[f.To] > l.layer[f.From] {
			sx, tx := s.x+s.w, t.x
			if s.cy() == t.cy() {
				pts = [][2]int{{sx, s.cy()}, {tx, t.cy()}}
			} else {
				mx := tx - 30
				pts = [][2]int{{sx, s.cy()}, {mx, s.cy()}, {mx, t.cy()}, {tx, t.cy()}}
			}
		} else { // a loop back: leave below the source, run under both and come up into the target
			by := s.y + s.h
			if t.y+t.h > by {
				by = t.y + t.h
			}
			by += 25
			pts = [][2]int{{s.cx(), s.y + s.h}, {s.cx(), by}, {t.cx(), by}, {t.cx(), t.y + t.h}}
		}
		w("      <bpmndi:BPMNEdge id=\"%s_di\" bpmnElement=\"%s\">\n", fid(i), fid(i))
		for _, p := range pts {
			w("        <di:waypoint x=\"%d\" y=\"%d\" />\n", p[0], p[1])
		}
		w("      </bpmndi:BPMNEdge>\n")
	}
	w("    </bpmndi:BPMNPlane>\n  </bpmndi:BPMNDiagram>\n</bpmn:definitions>\n")
	return []byte(b.String())
}
