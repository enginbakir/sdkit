// Package state holds the per-task resumable state (<project-dir>/work/<slug>/state.json, what state.sh kept)
// and a reader for the open task of the phase machine (<project-dir>/.sdd/state.json, written by internal/sdd).
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/store"
)

// ErrMissing is returned when a task has no state file yet.
var ErrMissing = errors.New("state: no state file")

// State is work/<slug>/state.json. Keys set with `state set` that are not fields here are kept in Extra.
type State struct {
	Slug    string
	Step    string
	Risk    string
	Branch  string
	Verdict string
	Tasks   map[string]string
	Retries map[string]int
	Updated string
	Spawns  int // 0 = never counted, and then the key is absent
	Extra   map[string]json.RawMessage
}

// ValidSlug reports whether s may name a task directory.
func ValidSlug(s string) bool {
	return s != "" && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-") == ""
}

// Path is the state file of slug.
func Path(p project.Project, slug string) string {
	return filepath.Join(p.Dir(), "work", slug, "state.json")
}

// Now is the timestamp format of the file.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func enc(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return bytes.TrimSpace(b.Bytes())
}

// Marshal renders the file as jq printed it: fixed key order, two-space indent, extras last, no trailing newline.
func (s *State) Marshal() []byte {
	var c bytes.Buffer
	c.WriteByte('{')
	add := func(k string, v []byte) {
		if c.Len() > 1 {
			c.WriteByte(',')
		}
		c.Write(enc(k))
		c.WriteByte(':')
		c.Write(v)
	}
	add("slug", enc(s.Slug))
	add("step", enc(s.Step))
	add("risk", enc(s.Risk))
	add("branch", enc(s.Branch))
	add("verdict", enc(s.Verdict))
	add("tasks", enc(orEmpty(s.Tasks)))
	add("retries", enc(orEmpty(s.Retries)))
	add("updated", enc(s.Updated))
	if s.Spawns > 0 {
		add("spawns", enc(s.Spawns))
	}
	keys := make([]string, 0, len(s.Extra))
	for k := range s.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, s.Extra[k])
	}
	c.WriteByte('}')
	var out bytes.Buffer
	_ = json.Indent(&out, c.Bytes(), "", "  ")
	return out.Bytes()
}

func orEmpty[V any](m map[string]V) map[string]V {
	if m == nil {
		return map[string]V{}
	}
	return m
}

// Parse reads a state file; keys it does not know go to Extra.
func Parse(b []byte) (*State, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	s := &State{Extra: map[string]json.RawMessage{}}
	for k, v := range raw {
		var err error
		switch k {
		case "slug":
			err = json.Unmarshal(v, &s.Slug)
		case "step":
			err = json.Unmarshal(v, &s.Step)
		case "risk":
			err = json.Unmarshal(v, &s.Risk)
		case "branch":
			err = json.Unmarshal(v, &s.Branch)
		case "verdict":
			err = json.Unmarshal(v, &s.Verdict)
		case "tasks":
			err = json.Unmarshal(v, &s.Tasks)
		case "retries":
			err = json.Unmarshal(v, &s.Retries)
		case "updated":
			err = json.Unmarshal(v, &s.Updated)
		case "spawns":
			err = json.Unmarshal(v, &s.Spawns)
		default:
			s.Extra[k] = v
		}
		if err != nil {
			return nil, fmt.Errorf("state: %s: %w", k, err)
		}
	}
	return s, nil
}

// Read loads the state of slug (ErrMissing when there is none).
func Read(p project.Project, slug string) (*State, error) {
	b, err := os.ReadFile(Path(p, slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Init creates the state of slug and keeps an existing one.
func Init(p project.Project, slug, risk, branch string) error {
	return store.Update(Path(p, slug), func(old []byte) ([]byte, error) {
		if old != nil {
			return old, nil
		}
		return append((&State{Slug: slug, Step: "0-intake", Risk: risk, Branch: branch, Updated: Now()}).Marshal(), '\n'), nil
	})
}

// Update runs fn on the state of slug under the lock and stamps Updated. fn's error leaves the file as it was.
func Update(p project.Project, slug string, fn func(*State) error) error {
	return store.Update(Path(p, slug), func(old []byte) ([]byte, error) {
		if old == nil {
			return nil, ErrMissing
		}
		s, err := Parse(old)
		if err != nil {
			return nil, err
		}
		if err := fn(s); err != nil {
			return nil, err
		}
		s.Updated = Now()
		return append(s.Marshal(), '\n'), nil
	})
}

// Get returns a top-level value as `jq -r '.[k] // empty'` would: strings bare, null/false/missing empty, the rest as JSON.
func (s *State) Get(k string) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(s.Marshal(), &m)
	v, ok := m[k]
	var str string
	switch {
	case !ok || string(v) == "null" || string(v) == "false":
		return ""
	case json.Unmarshal(v, &str) == nil:
		return str
	}
	var out bytes.Buffer
	_ = json.Indent(&out, v, "", "  ")
	return out.String()
}

// Set stores a string value; tasks and retries have their own operations.
func (s *State) Set(k, v string) error {
	switch k {
	case "tasks", "retries":
		return fmt.Errorf("use the task/retry commands for %s", k)
	case "spawns":
		return errors.New("spawns is counted by the spawn command")
	case "slug":
		s.Slug = v
	case "step":
		s.Step = v
	case "risk":
		s.Risk = v
	case "branch":
		s.Branch = v
	case "verdict":
		s.Verdict = v
	case "updated":
		s.Updated = v
	default:
		if s.Extra == nil {
			s.Extra = map[string]json.RawMessage{}
		}
		s.Extra[k] = enc(v)
	}
	return nil
}

// SetTask records a task status (todo|doing|done|blocked).
func (s *State) SetTask(id, status string) error {
	switch status {
	case "todo", "doing", "done", "blocked":
	default:
		return errors.New("status must be todo|doing|done|blocked")
	}
	if s.Tasks == nil {
		s.Tasks = map[string]string{}
	}
	s.Tasks[id] = status
	return nil
}

// UseRetry counts the one automatic retry of id; false when it is already used.
func (s *State) UseRetry(id string) bool {
	if s.Retries[id] >= 1 {
		return false
	}
	if s.Retries == nil {
		s.Retries = map[string]int{}
	}
	s.Retries[id] = 1
	return true
}

// Open is the task the phase machine has open (.sdd/state.json); only the fields policy needs.
type Open struct{ Task, Risk, Profile, Phase string }

// ReadOpen reads <project-dir>/.sdd/state.json; the zero Open (profile standard) when absent or unreadable.
func ReadOpen(p project.Project) Open {
	o := Open{Profile: "standard"}
	b, err := os.ReadFile(filepath.Join(p.Dir(), ".sdd", "state.json"))
	if err != nil {
		return o
	}
	var f struct{ Task, Risk, Profile, Phase string }
	if json.Unmarshal(b, &f) != nil {
		return o
	}
	o = Open(f)
	if o.Profile == "" {
		o.Profile = "standard"
	}
	return o
}

// ActiveSlug is the task whose budget and ledger count: the open task when it has a work state, else the most recently updated one; "" if none.
func ActiveSlug(p project.Project) string {
	if t := ReadOpen(p).Task; ValidSlug(t) {
		if _, err := os.Stat(Path(p, t)); err == nil {
			return t
		}
	}
	files, _ := filepath.Glob(filepath.Join(p.Dir(), "work", "*", "state.json"))
	best, bt := "", time.Time{}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && (best == "" || st.ModTime().After(bt)) {
			best, bt = filepath.Base(filepath.Dir(f)), st.ModTime()
		}
	}
	return best
}
