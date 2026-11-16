// Package stack defines what a language stack provides to the verify engine, the
// hooks and init: detection, the tools it needs, a formatter and its verify steps.
// Each stack is one file in this package and registers itself from init().
package stack

import (
	"context"
	"sort"

	"github.com/wecii/sdkit/internal/envfile"
)

// Tool is one line of a stack's tool list (`required|name|hint` in 1.x).
type Tool struct {
	Required bool   // false: optional, the stack degrades without it
	Name     string // executable name, looked up with proc.LookPath
	Hint     string // how to install it
}

// Env is what Steps gets: the repo and the changed set the engine computed.
type Env struct {
	Root      string // repository root; steps run from here
	KitDir    string // project directory relative to Root
	MergeBase string // the ref the changed set is relative to
	Fast      bool   // cheap checks only
	Changed   []string
	Source    []string // Changed, filtered by Stack.IsSource
	Settings  *envfile.Settings
}

// Reporter is how steps talk to the engine, so output and counts stay uniform.
type Reporter interface {
	Step(name string)                                                // prints "==> name"
	Fail(format string, a ...any)                                    // counts a failure
	Warn(format string, a ...any)                                    // counts a warning
	Exec(ctx context.Context, dir, name string, args ...string) bool // runs a program; on failure prints the tail of its output; true when it exited 0
	Shell(ctx context.Context, dir, cmd string) bool                 // the same for a user command string (proc.Shell)
}

// Stack is one language stack.
type Stack interface {
	Name() string
	Detect(root string) bool // does root look like this stack's project
	Tools() []Tool
	IsSource(path string) bool // is path one of this stack's source files
	Fmt(file string) error     // format one file in place; nil when there is nothing to do
	Steps(ctx context.Context, e Env, r Reporter)
}

var registry = map[string]Stack{}

// Register adds a stack; call it from an init function.
func Register(s Stack) { registry[s.Name()] = s }

// Get returns the stack called name, or nil.
func Get(name string) Stack { return registry[name] }

// Names returns the registered stack names, sorted.
func Names() []string {
	n := make([]string, 0, len(registry))
	for k := range registry {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
