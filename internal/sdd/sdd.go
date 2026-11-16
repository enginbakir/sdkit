// Package sdd is the phase machine of the spec-driven workflow: the only thing that moves a task forward, and the one
// decision point (Gate) that hooks, git hooks and CI ask. Later phases plug in through Telemetry, Reporter and Managed;
// a failure in any of them never changes a decision.
package sdd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
)

// Telemetry receives one event (type and alternating key, value fields) for the open task. P4 supplies it.
type Telemetry interface {
	Emit(typ string, fields ...string)
}

// Reporter writes the value report of the task that is ending (status: completed, aborted, failed, blocked, partial). P4 supplies it.
type Reporter interface{ Finalize(status string) }

// Refusal is a move or a record that the rules say no to. Reason is one word of the telemetry vocabulary.
type Refusal struct{ Reason, Msg string }

// Managed is the Jira path (P3-11). Nil means managed mode is absent: its commands print the usage, as the bash does
// when SDD_MANAGED is not on.
type Managed interface {
	// Command runs start --ticket, block, revalidate, evidence-sync or done and returns the exit code.
	Command(e *Env, cmd string, args []string) int
	// Rule is the extra text of `transitions` for a phase ("" none).
	Rule(phase string) string
	// Gate adds checks to advance (proposed, verified, pr, done); apply, when set, is written with the move.
	Gate(e *Env, phase string, t *Task) (apply func(*Task), r *Refusal)
	// Push refuses a push ("" allows); Edit refuses an edit of rel ("" allows).
	Push(e *Env, t *Task) string
	Edit(e *Env, rel string) string
}

type nop struct{}

func (nop) Emit(string, ...string) {}
func (nop) Finalize(string)        {}

// Tel, Rep and Mgd are the plug-in points; New copies them into each Env.
var (
	Tel Telemetry = nop{}
	Rep Reporter  = nop{}
	Mgd Managed
)

// PendingAgents lists the agents still in flight for a task (internal/contract supplies it); nil means none.
var PendingAgents func(task string) []string

// Phases in the only order they are walked.
var Phases = []string{"intake", "proposed", "red", "green", "verified", "pr", "done"}

// Env is one project and the settings it runs under.
type Env struct {
	Root     string
	P        project.Project
	Get      func(string) string // a setting: sdkit.env, sdkit.local.env, then the environment
	Out, Err io.Writer
	Tel      Telemetry
	Rep      Reporter
	Mgd      Managed
	ciBase   string // set by ci-check: the base the CI job gave
}

// Root finds the project root as the hooks do: CLAUDE_PROJECT_DIR, else the git top level, else the cwd.
func Root() string {
	if r := os.Getenv("CLAUDE_PROJECT_DIR"); r != "" {
		return r
	}
	cwd, _ := os.Getwd()
	if r, err := gitx.Top(cwd); err == nil {
		return r
	}
	return cwd
}

// New loads the project at root.
func New(root string) *Env {
	s, p, _ := project.Load(root)
	return &Env{Root: root, P: p, Get: base.Getter(s.Raw), Out: os.Stdout, Err: os.Stderr, Tel: Tel, Rep: Rep, Mgd: Mgd}
}

func (e *Env) setting(k, def string) string {
	if v := e.Get(k); v != "" {
		return v
	}
	return def
}

// Mode is the effective SDD_MODE: strict (default), guided or off.
func (e *Env) Mode() string { return e.setting("SDD_MODE", "strict") }

func (e *Env) managed() bool     { return e.Mgd != nil && e.Get("SDD_MANAGED") == "on" }
func (e *Env) stateDir() string  { return filepath.Join(e.P.Dir(), ".sdd") }
func (e *Env) statePath() string { return filepath.Join(e.stateDir(), "state.json") }
func (e *Env) abs(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(e.Root, rel)
}

// kitPath is the project directory name in slash form, for pathspecs and messages.
func (e *Env) kitPath() string { return filepath.ToSlash(e.P.KitDir) }

func (e *Env) printf(f string, a ...any) { fmt.Fprintf(e.Out, f, a...) }
func (e *Env) warnf(f string, a ...any)  { fmt.Fprintf(e.Err, f, a...) }
func (e *Env) die(msg string) int        { fmt.Fprintf(e.Err, "sdd: %s\n", msg); return 1 }
func (e *Env) emit(typ string, f ...string) {
	defer func() { _ = recover() }()
	e.Tel.Emit(typ, f...)
}

func (e *Env) noTask() int {
	return e.die("no active task. Start one: sdkit sdd start <slug> --risk <low|medium|high> --proposal <path>")
}

func tail(path string, n int) string {
	b, _ := os.ReadFile(path)
	if len(b) == 0 {
		return ""
	}
	l := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n") + "\n"
}

func rank(level string) int {
	switch level {
	case "medium":
		return 1
	case "high":
		return 2
	}
	return 0
}

// Phase is the phase of the open task ("" when none is open).
func (e *Env) Phase() string {
	if t := e.load(); t != nil {
		return t.Phase
	}
	return ""
}

// TaskSlug is the slug of the open task ("" when none is open).
func (e *Env) TaskSlug() string {
	if t := e.load(); t != nil {
		return t.Task
	}
	return ""
}

// Die prints "sdd: msg" on stderr and returns exit code 1.
func (e *Env) Die(msg string) int { return e.die(msg) }

// Managed reports whether managed execution is on and supplied.
func (e *Env) Managed() bool { return e.managed() }

// ManagedCommand runs one of the managed commands (block, revalidate, evidence-sync, done).
func (e *Env) ManagedCommand(cmd string, args []string) int { return e.Mgd.Command(e, cmd, args) }
