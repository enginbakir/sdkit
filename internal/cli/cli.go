// Package cli holds the command registry and the exit codes.
package cli

import (
	"fmt"
	"os"
	"sort"
)

// Exit codes (docs/go-port/02-contracts.md).
const (
	ExitOK        = 0
	ExitFail      = 1 // failure or refusal
	ExitUsage     = 2
	ExitBudget    = 3
	ExitContract  = 4 // 4 and 5 are contract errors
	ExitContract2 = 5
	ExitCached    = 7 // valid evidence already exists, nothing was run
	ExitDeclined  = 8 // routing declined, no valid result
)

var commands = map[string]func(args []string) int{}

// Register adds a command; call it from an init function.
func Register(name string, run func(args []string) int) {
	commands[name] = run
}

// Names returns the registered command names, sorted.
func Names() []string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Run dispatches args (without the program name) and returns the exit code.
func Run(args []string) int {
	name := "help"
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	run, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "sdkit: unknown command '%s' (try: sdkit help)\n", name)
		return ExitUsage
	}
	return run(args)
}
