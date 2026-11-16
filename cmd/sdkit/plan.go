package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("plan", runPlan) }

func planUsage(msg string) int {
	fmt.Fprintln(os.Stderr, "usage: "+msg)
	return cli.ExitUsage
}

func runPlan(args []string) int {
	if len(args) == 0 {
		return planUsage("sdkit plan parity|writes|waves ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "parity":
		const u = "sdkit plan parity plan.md proposal.md [tasks.md] [--loose]"
		var files []string
		loose := false
		for _, a := range rest {
			if a == "--loose" {
				loose = true
			} else {
				files = append(files, a)
			}
		}
		if len(files) < 2 || len(files) > 3 {
			return planUsage(u)
		}
		var txt [3]string
		for i, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				if i == 2 {
					fmt.Fprintln(os.Stderr, "tasks file not found: "+f)
					return cli.ExitUsage
				}
				return planUsage(u)
			}
			txt[i] = string(b)
		}
		out, code := plan.Parity(txt[0], txt[1], txt[2], len(files) == 3, loose)
		if code == 2 {
			side := map[string]string{"plan": files[0], "proposal": files[1]}[out[0]]
			fmt.Fprintln(os.Stderr, "no DoD items (D<n>: ...) found in "+side)
			return code
		}
		for _, l := range out {
			fmt.Println(l)
		}
		return code
	case "writes":
		if len(rest) < 3 || len(rest) > 4 {
			return planUsage("sdkit plan writes tasks.md task-id base-ref [head-ref]")
		}
		b, err := os.ReadFile(rest[0])
		if err != nil || rest[1] == "" || rest[2] == "" {
			return planUsage("sdkit plan writes tasks.md task-id base-ref [head-ref]")
		}
		var changed []string
		if len(rest) == 4 {
			out, err := gitx.Run(".", "diff", "--name-only", rest[2], rest[3])
			if err != nil {
				fmt.Fprintln(os.Stderr, "sdkit: git diff failed:", err)
				return cli.ExitFail
			}
			changed = gitx.Lines(out)
		} else {
			fs, err := gitx.ChangedFiles(".", rest[2], true)
			if err != nil {
				fmt.Fprintln(os.Stderr, "sdkit: git diff failed:", err)
				return cli.ExitFail
			}
			sort.Strings(fs)
			for i, f := range fs {
				if i == 0 || f != fs[i-1] {
					changed = append(changed, f)
				}
			}
		}
		bad, writes, found := plan.CheckWrites(string(b), rest[1], changed)
		if !found {
			fmt.Fprintf(os.Stderr, "task %s not found in %s\n", rest[1], rest[0])
			return cli.ExitUsage
		}
		if len(bad) == 0 {
			return cli.ExitOK
		}
		if writes == "" {
			writes = "none"
		}
		fmt.Printf("task %s changed files outside writes: [%s]:\n", rest[1], writes)
		for _, f := range bad {
			fmt.Println("  " + f)
		}
		return cli.ExitFail
	case "waves":
		if len(rest) != 1 {
			return planUsage("sdkit plan waves <tasks.md>")
		}
		b, err := os.ReadFile(rest[0])
		if err != nil {
			return planUsage("sdkit plan waves <tasks.md>")
		}
		maxPar, set := os.Getenv("MAX_PARALLEL"), os.Getenv("MAX_PARALLEL") != ""
		if !set {
			dir, _ := filepath.Abs(filepath.Dir(rest[0]))
			if top, err := project.Root(dir); err == nil {
				if s, _, _ := project.Load(top); s != nil {
					maxPar = s.String("MAX_PARALLEL")
				}
			}
		}
		n, _ := strconv.Atoi(maxPar)
		ts, err := plan.ParseTasks(string(b))
		var out []string
		if err == nil {
			out, err = plan.Waves(ts, n)
		}
		for _, l := range out {
			fmt.Println(l)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitFail
		}
		return cli.ExitOK
	}
	return planUsage("sdkit plan parity|writes|waves ...")
}
