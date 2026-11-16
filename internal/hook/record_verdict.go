package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/verdict"
)

func init() { Register(SubagentStop, nil, "record-verdict", recordVerdict) }

// roleOf drops a plugin prefix ("kit:verifier") and a "-deep" suffix.
func roleOf(agent string) string {
	if i := strings.LastIndex(agent, ":"); i >= 0 {
		agent = agent[i+1:]
	}
	return strings.TrimSuffix(agent, "-deep")
}

// lastText is the answer of the stopping agent: last_assistant_message, else the last text the
// agent wrote in its transcript.
func lastText(in *Input) string {
	if m := in.Str("last_assistant_message"); m != "" {
		return m
	}
	f, err := os.Open(first(in.Str("agent_transcript_path"), in.Str("transcript_path")))
	if err != nil {
		return ""
	}
	defer f.Close()
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "assistant" {
			continue
		}
		var parts []struct{ Type, Text string }
		if json.Unmarshal(l.Message.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			if p.Type == "text" {
				last = p.Text
			}
		}
	}
	return last
}

// recordVerdict records the verdict of a verifier, reviewer or security-auditor against the diff as
// it is now, with the host's agent_id as the run id (only a registered run counts). It never blocks.
func recordVerdict(in *Input) Decision {
	role := roleOf(first(in.Str("agent_type"), in.Str("subagent_type")))
	switch role {
	case "verifier", "reviewer", "security-auditor":
	default:
		return Decision{}
	}
	id := in.Str("agent_id")
	e := sdd.New(in.Root)
	statePath := filepath.Join(e.P.Dir(), ".sdd", "state.json")
	msg := lastText(in)
	if msg == "" { // a stop with no answer: its registration must not wait for a later run
		if _, err := os.Stat(statePath); id != "" && err == nil {
			e.SpawnClose([]string{"--role", role, "--source", "hook"})
		}
		return Decision{}
	}
	o := verdict.Options{Role: role, NoNormalize: os.Getenv("PARSE_NORMALIZE") == "0"}
	var st struct {
		Proposal string `json:"proposal"`
	}
	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if prop := filepath.Join(in.Root, st.Proposal); role == "verifier" && st.Proposal != "" && fileOK(prop) {
		o.ProposalPath = prop
		o.Accepted = func(proposal, id string) bool {
			b, err := os.ReadFile(proposal)
			if err != nil {
				return false
			}
			line := plan.Item(string(b), id)
			return line != "" && plan.Accepted(filepath.Join(filepath.Dir(proposal), "acceptances.jsonl"), id, line)
		}
	}
	_ = os.Chdir(in.Root) // the verdict checks resolve evidence files from the project
	r := verdict.ParseWith(msg, o)
	if r.Kind != "" {
		if setting(in, "TELEMETRY") == "1" {
			reason := "malformed"
			if r.Kind == verdict.KindSemantic {
				reason = "inconsistent"
			}
			sdd.Tel.Emit("verify.result", "outcome", "malformed", "source", "verifier")
			sdd.Tel.Emit("failure", "category", "verdict-malformed", "reason", reason, "role", role, "source", "verifier")
		}
		format := "findings, SUMMARY, NO FINDINGS (when empty) and VERDICT"
		switch role {
		case "verifier":
			format = "DOD / FINDINGS / VERIFY / VERDICT"
		case "reviewer":
			format = "findings, SUMMARY and VERDICT"
		}
		return Decision{Message: fmt.Sprintf("%s verdict NOT recorded: %s. Ask the %s once for the exact %s format.", role, r.Line(), role, format)}
	}
	rec := sdd.Recorder{Env: func() *sdd.Env { return e }}
	if m, refused := rec.Record(first(r.Role, role), r.Verdict, "hook", id, ""); refused {
		return Decision{Message: fmt.Sprintf("%s verdict NOT recorded as evidence: %s\n%s well-formed, NOT recorded: %s. It does not count: run the %s again on the current diff.", role, r.Line(), r.Verdict, m, role)}
	}
	return Decision{}
}

func fileOK(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
