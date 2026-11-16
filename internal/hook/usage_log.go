package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/usage"
)

func init() {
	Register(SubagentStop, nil, "usage-log", usageLog(SubagentStop))
	Register(Stop, nil, "usage-log", usageLog(Stop))
}

// usageLog appends the token usage of the stopping agent (SubagentStop) or the session so far (Stop) from its transcript to the ledger and, with
// TELEMETRY=1, an agent.end event. Off unless USAGE_LOG=1, TELEMETRY=1 or USAGE_LINE=1. Never blocks: any problem is silence.
func usageLog(event string) Guard {
	return func(in *Input) Decision {
		if in.Event != event {
			return Decision{}
		}
		tel, line := setting(in, "TELEMETRY") == "1", setting(in, "USAGE_LINE") == "1"
		if setting(in, "USAGE_LOG") != "1" && !tel && !line {
			return Decision{}
		}
		sub := event == SubagentStop
		if !sub && os.Getenv("SDKIT_CHILD_RUN") != "" { // run-role accounts that child itself
			return Decision{}
		}
		e := sdd.New(in.Root)
		session := in.Str("session_id")
		var transcript, role, id, kind string
		if sub {
			transcript, role, id, kind = in.Str("agent_transcript_path"), first(in.Str("agent_type"), in.Str("subagent_type")), in.Str("agent_id"), "final"
			if !isFile(transcript) && id != "" { // the subagent's own transcript only, never the parent's
				if parent := in.Str("transcript_path"); parent != "" {
					transcript = strings.TrimSuffix(slash(parent), ".jsonl") + "/subagents/agent-" + id + ".jsonl"
				}
			}
		} else {
			transcript, role, id, kind = in.Str("transcript_path"), "orchestrator", "session-"+session, "snapshot"
		}
		if i := strings.LastIndex(role, ":"); i >= 0 {
			role = role[i+1:]
		}
		if role == "" && sub {
			if b, err := os.ReadFile(strings.TrimSuffix(transcript, ".jsonl") + ".meta.json"); err == nil {
				var m struct {
					AgentType string `json:"agentType"`
				}
				_ = json.Unmarshal(b, &m)
				role = m.AgentType[strings.LastIndex(m.AgentType, ":")+1:]
			}
		}
		if role == "" {
			role = "subagent"
		}
		slug := usage.CurrentSlug(e.P)
		put := func(r, id, kind string, t *usage.Totals) {
			_ = os.MkdirAll(filepath.Join(e.P.Dir(), "work", slug), 0o755)
			bm := t.ByModel
			if len(bm) == 0 {
				bm = []usage.ModelTotals{{}}
			}
			for _, m := range bm { // one line per model: a session that switches model is priced per model
				_ = usage.Log(usage.LedgerFile(e.P, slug), usage.Line{V: 2, TS: usage.Now(), Role: r, Host: "claude", Model: m.Model, Provider: t.Provider, Session: session,
					ID: id, Origin: "sdk", Kind: kind, Source: event, In: usage.Num(m.In), Out: usage.Num(m.Out), CacheRead: usage.Num(m.CacheRead), CacheWrite: usage.Num(m.CacheWrite)})
			}
		}
		say := func(t *usage.Totals) Decision {
			if !line || !sub {
				return Decision{}
			}
			if t == nil {
				return Decision{Message: fmt.Sprintf("[sdkit] %s done — token usage not available (the host gave no data)", role)}
			}
			all, hit := t.In+t.CacheRead+t.CacheWrite, "n/a"
			if all > 0 {
				hit = fmt.Sprintf("%d%%", t.CacheRead*100/all)
			}
			return Decision{Message: fmt.Sprintf("[sdkit] %s done — in: %s (cache read %s, write %s), out: %s, cache hit: %s", role, kfmt(all), kfmt(t.CacheRead), kfmt(t.CacheWrite), kfmt(t.Out), hit)}
		}
		if !isFile(transcript) { // an execution happened but its usage cannot be read: leave an unmeasured mark, never zero
			if sub && id != "" {
				_ = os.MkdirAll(filepath.Join(e.P.Dir(), "work", slug), 0o755)
				_ = usage.Log(usage.LedgerFile(e.P, slug), usage.Line{V: 2, TS: usage.Now(), Role: role, Host: "claude", Session: session, ID: id, Origin: "sdk", Kind: "final", TQ: "unavailable", Source: event})
			}
			return say(nil)
		}
		since := "" // the orchestrator's transcript is cumulative over every task: count from the open task's start
		if !sub {
			since = usage.Window(e.P, slug)
		}
		t, err := usage.Extract(transcript, since)
		if err != nil || t.Messages == 0 {
			return say(nil)
		}
		if session == "" {
			session = t.Session
		}
		if tel {
			tc, dur := transcriptStats(transcript)
			f := []string{"role", role, "host", "claude", "model", t.Model, "in", itoa(t.In), "out", itoa(t.Out), "cache-read", itoa(t.CacheRead), "cache-write", itoa(t.CacheWrite),
				"tool-calls", itoa(tc), "q", "tokens=actual", "q", "tool_calls=actual", "q", "duration=estimated", "source", event}
			if dur >= 0 {
				f = append(f, "dur-ms", itoa(dur))
			}
			if id != "" {
				f = append(f, "span", id)
			}
			sdd.Tel.Emit("agent.end", f...)
		}
		put(role, id, kind, t)
		if line && sub { // refresh the orchestrator's own row too, so a phase move right after shows current numbers
			if p := in.Str("transcript_path"); isFile(p) {
				if px, err := usage.Extract(p, usage.Window(e.P, slug)); err == nil && px.Messages > 0 {
					put("orchestrator", "session-"+session, "snapshot", px)
				}
			}
		}
		return say(t)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func isFile(p string) bool { st, err := os.Stat(p); return p != "" && err == nil && !st.IsDir() }

func kfmt(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return itoa(n)
}

// transcriptStats counts the distinct tool calls and the span between the first and last timestamp in ms (-1 when it cannot be told).
func transcriptStats(path string) (calls, durMs int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, -1
	}
	defer f.Close()
	ids := map[string]bool{}
	var lo, hi time.Time
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type, Timestamp string
			Message         struct{ Content json.RawMessage }
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, l.Timestamp); err == nil {
			if n == 0 || ts.Before(lo) {
				lo = ts
			}
			if n == 0 || ts.After(hi) {
				hi = ts
			}
			n++
		}
		if l.Type != "assistant" {
			continue
		}
		var parts []struct{ Type, ID string }
		if json.Unmarshal(l.Message.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "tool_use" {
					ids[p.ID] = true
				}
			}
		}
	}
	if n < 2 {
		return len(ids), -1
	}
	return len(ids), int(hi.Sub(lo).Milliseconds())
}
