package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
	"github.com/wecii/sdkit/internal/telemetry" // its init wires sdd.Tel
)

func init() { cli.Register("telemetry", runTelemetry) }

const telemetryUsage = `telemetry emit <type> [--task S] [--run ID] [--span ID] [--phase P] [--role R] [--dur-ms N] [--in N] [--out N] [--outcome O] [--reason R] [--q metric=actual|estimated|unavailable] [--force] [--strict] ...
telemetry now-ms | run-id [slug] | validate [slug|--file F]`

func runTelemetry(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, telemetryUsage)
		return cli.ExitUsage
	}
	root := sdd.Root()
	switch args[0] {
	case "now-ms":
		fmt.Println(telemetry.NowMs())
	case "run-id":
		slug := ""
		if len(args) > 1 {
			slug = args[1]
		}
		if slug == "" {
			slug = state.ActiveSlug(project.Resolve(root))
		}
		if !state.ValidSlug(slug) {
			slug = "_misc"
		}
		fmt.Println(telemetry.RunID(root, slug))
	case "emit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, telemetryUsage)
			return cli.ExitUsage
		}
		var f []string
		force, strict := false, false
		for i := 2; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--force":
				force = true
			case a == "--strict":
				strict = true
			case strings.HasPrefix(a, "--"):
				v := ""
				if i+1 < len(args) {
					v = args[i+1]
				}
				f = append(f, a[2:], v)
				i++
			}
		}
		if err := telemetry.Emit(root, args[1], f, force, strict); err != nil && strict {
			fmt.Fprintln(os.Stderr, "telemetry emit:", err)
			return cli.ExitUsage
		}
	case "validate":
		return validateEvents(root, args[1:])
	default:
		fmt.Fprintln(os.Stderr, telemetryUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

func validateEvents(root string, args []string) int {
	p := project.Resolve(root)
	file := ""
	if len(args) > 1 && args[0] == "--file" {
		file = args[1]
	} else {
		slug := state.ActiveSlug(p)
		if len(args) > 0 {
			slug = args[0]
		}
		file = filepath.Join(p.Dir(), "work", slug, "events.jsonl")
	}
	fh, err := os.Open(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "telemetry validate:", err)
		return cli.ExitFail
	}
	defer fh.Close()
	bad, n := 0, 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		n++
		if errs := telemetry.ValidateLine(sc.Bytes()); len(errs) > 0 {
			bad++
			fmt.Printf("line %d: %s\n", n, strings.Join(errs, "; "))
		}
	}
	if bad > 0 {
		return cli.ExitFail
	}
	fmt.Printf("%d events valid\n", n)
	return cli.ExitOK
}
