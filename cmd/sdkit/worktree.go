package main

import (
	"fmt"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/worktree"
)

func init() { cli.Register("worktree", runWorktree) }

const worktreeUsage = "usage: sdkit worktree add|path|merge|remove|clean|list [task-id]"

func runWorktree(args []string) int {
	sub, id := "", ""
	if len(args) > 0 {
		sub = args[0]
	}
	if len(args) > 1 {
		id = args[1]
	}
	fail := func(err error) int { fmt.Fprintln(os.Stderr, "sdkit:", err); return cli.ExitFail }
	switch sub {
	case "add", "path", "merge", "remove", "clean", "list":
	default:
		fmt.Fprintln(os.Stderr, worktreeUsage)
		return cli.ExitUsage
	}
	if (sub == "add" || sub == "merge" || sub == "remove") && !worktree.ValidID(id) {
		fmt.Fprintln(os.Stderr, "usage: sdkit worktree "+sub+" <task-id>  (letters, digits, _ . - only)")
		return cli.ExitUsage
	}
	wd, _ := os.Getwd()
	t, err := worktree.Open(wd)
	if err != nil {
		return fail(err)
	}
	switch sub {
	case "add":
		p, err := t.Add(id)
		if err != nil {
			return fail(err)
		}
		fmt.Println(p)
	case "path":
		p, err := t.Existing(id)
		if err != nil {
			return fail(err)
		}
		fmt.Println(p)
	case "merge":
		tasks := os.Getenv("TASKS_FILE")
		if len(args) > 2 {
			tasks = args[2]
		}
		var check func([]string) error
		if tasks != "" {
			check = func(changed []string) error {
				b, err := os.ReadFile(tasks)
				if err != nil {
					return fmt.Errorf("task %s not found in %s", id, tasks)
				}
				bad, writes, found := plan.CheckWrites(string(b), id, changed)
				if !found {
					return fmt.Errorf("task %s not found in %s", id, tasks)
				}
				if len(bad) > 0 {
					if writes == "" {
						writes = "none"
					}
					fmt.Printf("task %s changed files outside writes: [%s]:\n", id, writes)
					for _, f := range bad {
						fmt.Println(" ", f)
					}
					return fmt.Errorf("task %s left its writes: set; not merged. Fix the plan or revert the stray files.", id)
				}
				return nil
			}
		}
		if err := t.MergeChecked(id, check); err != nil {
			return fail(err)
		}
	case "remove":
		if err := t.Remove(id, false); err != nil {
			return fail(err)
		}
	case "clean":
		for _, d := range t.Clean() {
			fmt.Println("removed", d)
		}
	case "list":
		l := t.List()
		if len(l) == 0 {
			fmt.Println("no task worktrees")
		}
		for _, x := range l {
			fmt.Println(x)
		}
	}
	return cli.ExitOK
}
