package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/spec"
)

// specCmds maps `sdkit spec <verb>` to its handler; another file adds its verbs from its own init().
var specCmds = map[string]func(args []string) int{
	"change": func(a []string) int { return spec.RunChange(a, os.Stdout, os.Stderr) },
	"apply":  func(a []string) int { return spec.RunApply(a, os.Stdout, os.Stderr) },
}

func init() { cli.Register("spec", runSpec) }

func runSpec(args []string) int {
	if len(args) > 0 {
		if run, ok := specCmds[args[0]]; ok {
			return run(args[1:])
		}
	}
	verbs := make([]string, 0, len(specCmds))
	for v := range specCmds {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)
	fmt.Fprintf(os.Stderr, "usage: sdkit spec %s ...\n", strings.Join(verbs, "|"))
	return cli.ExitUsage
}
