package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// ToolUse is the context cost of one tool: its results, in estimated tokens (characters / 4), and what carrying them cost.
type ToolUse struct {
	Tool   string `json:"tool"`
	Calls  int    `json:"calls"`
	Tokens int    `json:"tokens"`
	Reread int    `json:"reread"`
	Share  int    `json:"share"`
}

// Result1 is one tool result.
type Result1 struct {
	Tool       string `json:"tool"`
	Label      string `json:"label"` // a command or path, cut at 70 characters; never a prompt or file content
	Tokens     int    `json:"tokens"`
	LaterTurns int    `json:"later_turns"`
	Reread     int    `json:"reread"`
}

// ContextReport is where a session's context came from (all token figures are estimates).
type ContextReport struct {
	Turns        int       `json:"transcript_turns"`
	Results      int       `json:"results"`
	ResultTokens int       `json:"result_tokens"`
	RereadTokens int       `json:"reread_tokens"`
	ByTool       []ToolUse `json:"by_tool"`
	Largest      []Result1 `json:"largest"`
}

func size(v any) int {
	switch t := v.(type) {
	case string:
		return utf8.RuneCountInString(t)
	case []any:
		n := 0
		for _, e := range t {
			if m := obj(e); m != nil {
				n += utf8.RuneCountInString(str(m["text"]))
			} else {
				b, _ := json.Marshal(e)
				n += utf8.RuneCount(b)
			}
		}
		return n
	}
	b, _ := json.Marshal(v)
	return utf8.RuneCount(b)
}

// Context reads a Claude Code transcript. Re-read = the result's tokens x the turns that came after it (what it cost to carry it).
func Context(path string, top int) (*ContextReport, error) {
	recs, err := records(path)
	if err != nil {
		return nil, err
	}
	var rec []map[string]any
	for _, r := range recs {
		if m := obj(r); m != nil {
			rec = append(rec, m)
		}
	}
	type use struct{ name, label string }
	uses := map[string]use{}
	for _, m := range rec {
		if str(m["type"]) != "assistant" {
			continue
		}
		cs, _ := obj(m["message"])["content"].([]any)
		for _, c := range cs {
			if c := obj(c); c != nil && str(c["type"]) == "tool_use" {
				in := obj(c["input"])
				l := ""
				if v := first(in["command"], in["file_path"], in["pattern"], in["description"]); v != nil {
					l = fmt.Sprint(v)
					if s, ok := v.(string); ok {
						l = s
					}
				}
				l = strings.NewReplacer("\n", " ", "\r", " ").Replace(l)
				if r := []rune(l); len(r) > 70 {
					l = string(r[:70])
				}
				uses[str(c["id"])] = use{str(c["name"]), l}
			}
		}
	}
	// turns after record i: distinct requests (requestId, else message id) of assistant records with usage, counted at their first record
	seen := map[string]bool{}
	cum := make([]int, len(rec))
	turns := 0
	for i, m := range rec {
		mm := obj(m["message"])
		if k := first(m["requestId"], mm["id"]); str(m["type"]) == "assistant" && obj(mm["usage"]) != nil && k != nil && !seen[str(k)] {
			seen[str(k)] = true
			turns++
		}
		cum[i] = turns
	}
	var rs []Result1
	for i, m := range rec {
		if str(m["type"]) != "user" {
			continue
		}
		cs, _ := obj(m["message"])["content"].([]any)
		for _, c := range cs {
			if c := obj(c); c != nil && str(c["type"]) == "tool_result" {
				u, ok := uses[str(c["tool_use_id"])]
				if !ok {
					u.name = "?"
				}
				r := Result1{Tool: u.name, Label: u.label, Tokens: size(c["content"]) / 4, LaterTurns: turns - cum[i]}
				r.Reread = r.Tokens * r.LaterTurns
				rs = append(rs, r)
			}
		}
	}
	rep := &ContextReport{Turns: turns, Results: len(rs), ByTool: []ToolUse{}, Largest: []Result1{}}
	tools := map[string]*ToolUse{}
	for _, r := range rs {
		rep.ResultTokens += r.Tokens
		rep.RereadTokens += r.Reread
		t := tools[r.Tool]
		if t == nil {
			t = &ToolUse{Tool: r.Tool}
			tools[r.Tool] = t
		}
		t.Calls++
		t.Tokens += r.Tokens
		t.Reread += r.Reread
	}
	for _, k := range sortedKeys(tools) {
		t := tools[k]
		if rep.ResultTokens > 0 {
			t.Share = t.Tokens * 100 / rep.ResultTokens
		}
		rep.ByTool = append(rep.ByTool, *t)
	}
	sort.SliceStable(rep.ByTool, func(i, j int) bool { return rep.ByTool[i].Reread > rep.ByTool[j].Reread })
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Reread > rs[j].Reread })
	rep.Largest = append(rep.Largest, rs[:min(top, len(rs))]...)
	return rep, nil
}

// Text is the readable form: totals, the tools by re-read cost, the largest single results.
func (c *ContextReport) Text() string {
	b := []string{fmt.Sprintf("session context: %d turns, %d tool results, ~%d tokens of results; carrying them cost ~%d re-read tokens (tokens x the turns after each result)", c.Turns, c.Results, c.ResultTokens, c.RereadTokens)}
	rows := [][]string{{"tool", "calls", "~tokens", "share", "~re-read"}}
	for _, t := range c.ByTool {
		rows = append(rows, []string{t.Tool, fmt.Sprint(t.Calls), fmt.Sprint(t.Tokens), fmt.Sprintf("%d%%", t.Share), fmt.Sprint(t.Reread)})
	}
	w := make([]int, 5)
	for _, r := range rows {
		for i, x := range r {
			w[i] = max(w[i], utf8.RuneCountInString(x))
		}
	}
	for _, r := range rows {
		var cells []string
		for i, x := range r {
			if i < 4 {
				x += strings.Repeat(" ", w[i]-utf8.RuneCountInString(x))
			}
			cells = append(cells, x)
		}
		b = append(b, strings.Join(cells, "  "))
	}
	b = append(b, "", "largest results by re-read cost:")
	for _, r := range c.Largest {
		b = append(b, fmt.Sprintf("  %s  ~%d tokens x %d later turns = %d   %s", r.Tool, r.Tokens, r.LaterTurns, r.Reread, r.Label))
	}
	return strings.Join(b, "\n") + "\n"
}

// LatestTranscript is the most recent session transcript of dir.
func LatestTranscript(dir string) (string, error) {
	l := newest(dir)
	if len(l) == 0 {
		return "", errors.New("context: no transcript (Claude Code only; pass a path)")
	}
	return l[0], nil
}
