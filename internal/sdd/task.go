package sdd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"

	"github.com/wecii/sdkit/internal/store"
)

// Task is <project-dir>/.sdd/state.json, the open task. Field names are the 1.x ones. Keys this package does not
// own (the managed mode keeps a few) are kept in Extra and written back.
type Task struct {
	Task                string                     `json:"task"`
	Risk                string                     `json:"risk"`
	Profile             string                     `json:"profile,omitempty"`
	Proposal            string                     `json:"proposal"`
	Phase               string                     `json:"phase"`
	StartedAt           string                     `json:"startedAt,omitempty"`
	History             []Step                     `json:"history,omitempty"`
	TestCmd             string                     `json:"testCmd,omitempty"`
	Verdict             *Verdict                   `json:"verdict,omitempty"`
	VerifiedFingerprint string                     `json:"verifiedFingerprint,omitempty"`
	Evidence            map[string]Record          `json:"evidence,omitempty"`
	EvSeq               int                        `json:"evseq,omitempty"`
	Spawns              []Spawn                    `json:"spawns,omitempty"`
	EvidenceLog         []LogEntry                 `json:"evidenceLog,omitempty"`
	UsedRuns            []UsedRun                  `json:"usedRuns,omitempty"`
	Managed             bool                       `json:"managed,omitempty"`
	Ticket              string                     `json:"ticket,omitempty"`
	SpecID              string                     `json:"specId,omitempty"`
	SpecVersion         string                     `json:"specVersion,omitempty"`
	SpecSha             string                     `json:"specSha,omitempty"`
	AcceptanceCriteria  []string                   `json:"acceptanceCriteria,omitempty"`
	Blocked             *Blocked                   `json:"blocked,omitempty"`
	PRHead              string                     `json:"prHead,omitempty"`
	PRBranch            string                     `json:"prBranch,omitempty"`
	PRBase              string                     `json:"prBase,omitempty"`
	Extra               map[string]json.RawMessage `json:"-"`
}

// Step is one entry of History.
type Step struct {
	Phase  string `json:"phase"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

// Verdict is the verifier's last result, bound to a diff.
type Verdict struct {
	Result      string `json:"result"`
	Fingerprint string `json:"fingerprint"`
	At          string `json:"at"`
}

// Record is the latest evidence of one role (trust: spawned | unregistered | manual).
type Record struct {
	Result      string `json:"result"`
	Fingerprint string `json:"fingerprint"`
	Task        string `json:"task"`
	Trust       string `json:"trust"`
	Source      string `json:"source"`
	Run         string `json:"run"`
	At          string `json:"at"`
	Seq         int    `json:"seq"`
}

// Spawn is the registration of an independent role run that started.
type Spawn struct {
	Role   string `json:"role"`
	Source string `json:"source"`
	Run    string `json:"run"`
	FP     string `json:"fp"`
	At     string `json:"at"`
}

// LogEntry is one attempt to record evidence, or one closed registration (last 40 are kept).
type LogEntry struct {
	N        string `json:"n,omitempty"`
	Role     string `json:"role"`
	Result   string `json:"result,omitempty"`
	Run      string `json:"run"`
	Source   string `json:"source,omitempty"`
	Trust    string `json:"trust,omitempty"`
	FP       string `json:"fp,omitempty"`
	At       string `json:"at"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}

// UsedRun remembers a run id that produced evidence, so a second role cannot reuse it.
type UsedRun struct {
	Run  string `json:"run"`
	Role string `json:"role"`
}

// Blocked is the managed-mode flag that stops every advance and gate until the spec is revalidated.
type Blocked struct {
	Kind         string  `json:"kind"`
	Ref          string  `json:"ref"`
	Reason       string  `json:"reason,omitempty"`
	Section      *string `json:"section"`
	Transitioned *bool   `json:"transitioned,omitempty"`
	At           string  `json:"at"`
}

var known = func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeOf(Task{})
	for i := 0; i < t.NumField(); i++ {
		if n, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); n != "" && n != "-" {
			m[n] = true
		}
	}
	return m
}()

// ParseTask reads a state file; unknown keys go to Extra.
func ParseTask(b []byte) (*Task, error) {
	type plain Task
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k := range m {
		if known[k] {
			delete(m, k)
		}
	}
	t := Task(p)
	t.Extra = m
	return &t, nil
}

// Marshal renders the file: two-space indent, keys sorted, a trailing newline.
func (t *Task) Marshal() []byte {
	b, _ := json.Marshal((*plainTask)(t))
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(b, &m)
	for k, v := range t.Extra {
		m[k] = v
	}
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(m)
	return out.Bytes()
}

type plainTask Task

var errNoTask = errors.New("no active task")

// load reads the open task; nil when none is open or the file is unreadable.
func (e *Env) load() *Task {
	b, err := os.ReadFile(e.statePath())
	if err != nil {
		return nil
	}
	t, err := ParseTask(b)
	if err != nil {
		return nil
	}
	return t
}

// put is a read-modify-write under the state lock: the verdict hook and the orchestrator can write at the same time.
// fn runs on the current state; an error from fn leaves the file untouched.
func (e *Env) put(fn func(*Task) error) error {
	return store.Update(e.statePath(), func(old []byte) ([]byte, error) {
		if old == nil {
			return nil, errNoTask
		}
		t, err := ParseTask(old)
		if err != nil {
			return nil, err
		}
		if err := fn(t); err != nil {
			return nil, err
		}
		return t.Marshal(), nil
	})
}
