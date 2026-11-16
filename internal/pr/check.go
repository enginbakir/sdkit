package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
)

// CheckOptions is `sdkit pr check`: cheap, risk-based checks of the kit or a repo that vendors it.
type CheckOptions struct {
	Dir, Base       string
	List, Milestone bool
	// Tool resolves a helper (test-select, audit, build-adapters, bench history) to an argv, or nil when absent.
	Tool func(name string) []string
	Out  io.Writer
}

// Check runs T0 (syntax of changed *.sh, validity of changed *.json, generated adapters, audit policy) and
// T1 (the suites mapped to the changed files, or the full suite and benchmark check with Milestone).
// It returns the number of failed steps.
func Check(ctx context.Context, o CheckOptions) (int, error) {
	top, err := gitx.Top(o.Dir)
	if err != nil {
		return 0, fail(2, "pr-check needs a git repository")
	}
	base := o.Base
	if base == "" {
		for _, c := range []string{"origin/main", "origin/master", "main", "master", "HEAD"} {
			if gitx.RevParse(top, c) != "" {
				base = c
				break
			}
		}
	}
	mb, err := gitx.MergeBase(top, base, "HEAD")
	if err != nil {
		mb = "HEAD"
	}
	files, _ := gitx.ChangedFiles(top, mb, true)
	seen := map[string]bool{}
	var sh, js []string
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		if _, err := os.Stat(filepath.Join(top, f)); err != nil {
			continue
		}
		switch filepath.Ext(f) {
		case ".sh":
			sh = append(sh, f)
		case ".json":
			js = append(js, f)
		}
	}
	tool := func(n string, a ...string) []string {
		if t := o.Tool(n); t != nil {
			return append(t, a...)
		}
		return nil
	}
	sel := tool("test-select", "run", "--base", mb)
	if o.Milestone {
		sel = tool("test-select", "run", "--tier", "T3", "--files", "tests/test-map.conf", "--no-cache")
	}
	if o.List {
		short := mb
		if len(short) > 8 {
			short = short[:8]
		}
		fmt.Fprintf(o.Out, "base %s (merge-base %s), %d changed file(s)\n", base, short, len(seen))
		fmt.Fprintf(o.Out, "T0: bash -n x%d, json x%d, build-adapters --check, audit policy\nT1:\n", len(sh), len(js))
		if l := tool("test-select", "list", "--base", mb); l != nil {
			res, _ := proc.Run(ctx, top, nil, l[0], l[1:]...)
			for _, ln := range gitx.Lines(res.Stdout) {
				fmt.Fprintln(o.Out, "  "+ln)
			}
		}
		if o.Milestone {
			fmt.Fprintln(o.Out, "T3: full suite + bench/history.sh check")
		}
		return 0, nil
	}
	fails, start := 0, time.Now()
	step := func(label string, f func() (string, bool)) {
		s := time.Now()
		log, ok := f()
		el := int(time.Since(s).Seconds())
		if ok {
			fmt.Fprintf(o.Out, "ok    %s (%ds)\n", label, el)
			return
		}
		fails++
		fmt.Fprintf(o.Out, "FAIL  %s (%ds)\n", label, el)
		lines := gitx.Lines(log)
		if len(lines) > 15 {
			lines = lines[len(lines)-15:]
		}
		for _, l := range lines {
			fmt.Fprintln(o.Out, "      "+l)
		}
	}
	exec := func(argv []string) func() (string, bool) {
		return func() (string, bool) {
			res, err := proc.Run(ctx, top, nil, argv[0], argv[1:]...)
			return res.Stdout + res.Stderr, err == nil && res.Code == 0
		}
	}
	step(fmt.Sprintf("T0 bash -n (%d files)", len(sh)), func() (string, bool) {
		var log strings.Builder
		if _, err := proc.LookPath("bash"); err != nil {
			return "bash not found, syntax check skipped", true // Windows without Git Bash
		}
		for _, f := range sh {
			if res, err := proc.Run(ctx, top, nil, "bash", "-n", f); err != nil || res.Code != 0 {
				log.WriteString(res.Stderr)
				return log.String(), false
			}
		}
		return "", true
	})
	step(fmt.Sprintf("T0 json (%d files)", len(js)), func() (string, bool) {
		var bad []string
		for _, f := range js {
			if b, err := os.ReadFile(filepath.Join(top, f)); err != nil || !json.Valid(b) {
				bad = append(bad, f)
			}
		}
		return strings.Join(bad, "\n"), len(bad) == 0
	})
	if t := tool("build-adapters", "--check"); t != nil {
		step("T0 adapters are generated from core/roles", exec(t))
	}
	if t := tool("audit", "policy"); t != nil {
		step("T0 shipped enforcement (audit policy)", exec(t))
	}
	if sel != nil {
		label := "T1 focused suites for the changed files"
		if o.Milestone {
			label = "T3 full suite (milestone)"
		}
		step(label, exec(sel))
	}
	tier := "T1"
	if o.Milestone {
		tier = "T3"
		if t := tool("bench-history", "check"); t != nil {
			step("T3 fixture benchmark regression", exec(t))
		}
	}
	res := "green"
	if fails > 0 {
		res = fmt.Sprintf("%d step(s) FAILED", fails)
	}
	fmt.Fprintf(o.Out, "pr-check: %s, %ds total; tier %s (a green T1 is not a full regression)\n", res, int(time.Since(start).Seconds()), tier)
	return fails, nil
}
