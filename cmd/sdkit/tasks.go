package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/tasks"
)

func init() { cli.Register("tasks", runTasks) }

// runTasks wires sdkit tasks. The approval gate is `sdkit spec approved-sha` and the
// contract check is `sdkit jira validate-task -`; SPEC_SH and JIRA_SH replace them (tests).
func runTasks(args []string) int {
	cwd, _ := os.Getwd()
	top := os.Getenv("CLAUDE_PROJECT_DIR")
	if top == "" {
		if t, err := gitx.Top(cwd); err == nil {
			top = t
		} else {
			top = cwd
		}
	}
	repo := top
	if r := os.Getenv("TASKS_REPO"); r != "" {
		repo = r
	}
	self, _ := os.Executable()
	call := func(env, sub string, stdin string, a ...string) (string, bool) {
		name, argv := self, append([]string{sub}, a...)
		if o := os.Getenv(env); o != "" {
			name, argv = o, a
		}
		cmd := exec.Command(name, argv...)
		cmd.Stdin = strings.NewReader(stdin)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err == nil
	}
	return tasks.Run(tasks.Env{
		Repo:     repo,
		SpecPath: os.Getenv("TASKS_SPEC_PATH"),
		Approve: func(id, sha string) bool {
			_, ok := call("SPEC_SH", "spec", "", "approved-sha", id, "--sha", sha)
			return ok
		},
		Validate: func(text string) (string, bool) {
			return call("JIRA_SH", "jira", text, "validate-task", "-")
		},
		Stdin: os.Stdin, Out: os.Stdout, Err: os.Stderr,
	}, args)
}
