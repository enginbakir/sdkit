package ui

import (
	"encoding/json"
	"errors"
	"strings"
)

// Finding is a review finding the agent asks about, in the fields the card shows. The agent fills it next to the question; the
// page draws it. Every field is optional except the problem: a card with nothing to say is just the question.
type Finding struct {
	ID       string `json:"id,omitempty"`
	Severity string `json:"severity,omitempty"` // BLOCKER | MAJOR | MINOR
	Where    string `json:"where,omitempty"`    // file, and line when known
	Problem  string `json:"problem"`
	Class    string `json:"class,omitempty"` // FIX | ASK | REJECT: the agent's own triage
	Diff     string `json:"diff,omitempty"`  // the proposed edit, unified-diff lines
}

// Option is one answer the person can pick.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionItem is one question, in the shape of the host's own question tool plus the optional finding.
type QuestionItem struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Finding     *Finding `json:"finding,omitempty"`
	Plan        *Plan    `json:"plan,omitempty"` // the plan gate of generate-tasks
}

var (
	severities = map[string]bool{"BLOCKER": true, "MAJOR": true, "MINOR": true}
	classes    = map[string]bool{"FIX": true, "ASK": true, "REJECT": true}
)

// clip cuts s to n runes. A finding is read by a person on a card; a reviewer's essay does not belong on it.
func clip(s string, n int) string {
	if r := []rune(strings.TrimSpace(s)); len(r) > n {
		return string(r[:n]) + "…"
	}
	return strings.TrimSpace(s)
}

// NormalizeQuestions parses what the agent sent, drops what the page cannot show, and bounds what it can. A malformed
// severity or class is dropped, not refused: the question itself still has to reach the person.
func NormalizeQuestions(raw []byte) ([]byte, error) {
	var in struct {
		Questions []QuestionItem `json:"questions"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || len(in.Questions) == 0 {
		return nil, errors.New(`expected {"questions":[{"question":"...","options":[{"label":"..."}]}]}`)
	}
	for i := range in.Questions {
		q := &in.Questions[i]
		if strings.TrimSpace(q.Question) == "" {
			return nil, errors.New("a question has no text")
		}
		q.Question = clip(q.Question, 2000)
		q.Header = clip(q.Header, 60)
		if len(q.Options) > 12 {
			q.Options = q.Options[:12]
		}
		for j := range q.Options {
			q.Options[j].Label, q.Options[j].Description = clip(q.Options[j].Label, 200), clip(q.Options[j].Description, 600)
		}
		q.Plan = normalizePlan(q.Plan)
		if f := q.Finding; f != nil {
			f.Problem = clip(f.Problem, 1500)
			if f.Problem == "" {
				q.Finding = nil
				continue
			}
			f.ID, f.Where, f.Diff = clip(f.ID, 60), clip(f.Where, 300), clip(f.Diff, 4000)
			if f.Severity = strings.ToUpper(strings.TrimSpace(f.Severity)); !severities[f.Severity] {
				f.Severity = ""
			}
			if f.Class = strings.ToUpper(strings.TrimSpace(f.Class)); !classes[f.Class] {
				f.Class = ""
			}
		}
	}
	return json.Marshal(in)
}
