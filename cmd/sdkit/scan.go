package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/secretscan"
)

func init() { cli.Register("scan", runScan) }

const scanUsage = "usage: sdkit scan secrets [--history [--since <ref>]] [--staged] [--json]"

// runScan is `sdkit scan secrets`: credential-shaped strings in the working tree (default), the staged change or the git history.
// Accepted findings live in <project-dir>/secret-baseline.json (committed); the findings of the run go to <project-dir>/work/scan/findings.json
// (git-ignored). Exit 0 = no open finding, 1 = at least one, 2 = usage or a failed scan. The scan never prints or stores a secret.
func runScan(args []string) int {
	if len(args) == 0 || args[0] != "secrets" {
		fmt.Fprintln(os.Stderr, scanUsage)
		return cli.ExitUsage
	}
	var o secretscan.Options
	asJSON := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--history":
			o.History = true
		case "--staged":
			o.Staged = true
		case "--json":
			asJSON = true
		case "--since":
			if i++; i == len(args) {
				fmt.Fprintln(os.Stderr, scanUsage)
				return cli.ExitUsage
			}
			o.Since = args[i]
		default:
			fmt.Fprintln(os.Stderr, scanUsage)
			return cli.ExitUsage
		}
	}
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit: not in a git repository")
		return cli.ExitUsage
	}
	o.Root = root
	p := project.Resolve(root)
	bl, err := secretscan.LoadBaseline(filepath.Join(p.Dir(), secretscan.BaselineFile))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitUsage
	}
	all, err := secretscan.Scan(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitUsage
	}
	open, accepted := bl.Split(all)
	rep := secretscan.Report{Findings: open, Baselined: accepted}
	if p.Found {
		dir := filepath.Join(p.Dir(), "work", "scan")
		if os.MkdirAll(dir, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(dir, "findings.json"), rep.JSON(), 0o644)
		}
	}
	if asJSON {
		os.Stdout.Write(rep.JSON())
	} else {
		for _, f := range open {
			loc := f.Path
			if f.Line > 0 {
				loc += fmt.Sprintf(":%d", f.Line)
			}
			if f.Commit != "" {
				loc = f.Commit + ":" + loc
			}
			fmt.Printf("%-6s %-26s %s  %s  [%s]\n", f.Severity, f.Rule, loc, f.Excerpt, f.Fingerprint)
		}
		fmt.Printf("scan secrets: %d open, %d in baseline\n", len(open), len(accepted))
	}
	if len(open) > 0 {
		return cli.ExitFail
	}
	return cli.ExitOK
}
