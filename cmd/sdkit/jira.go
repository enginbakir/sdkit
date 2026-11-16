package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/jira"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("jira", runJira) }

const jiraUsage = "usage: sdkit jira validate-task|fetch|deps-ready|upsert|comment|transition|change-request ..."

func runJira(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, jiraUsage)
		return cli.ExitUsage
	}
	verb, a := args[0], args[1:]
	arg := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	usage := func(u string) int { fmt.Fprintln(os.Stderr, "jira: usage: "+u); return cli.ExitUsage }
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		root = wd
	}
	c := jira.Load(root, os.Getenv)
	var out any
	switch verb {
	case "validate-task":
		var in []byte
		if f := arg(0); f != "" && f != "-" {
			if in, err = os.ReadFile(f); err != nil {
				fmt.Fprintln(os.Stderr, "jira: no such file: "+f)
				return cli.ExitUsage
			}
		} else {
			in, _ = io.ReadAll(os.Stdin)
		}
		out, err = jira.Validate(string(in))
	case "fetch":
		if arg(0) == "" {
			return usage("fetch KEY")
		}
		out, err = c.Fetch(a[0])
	case "deps-ready":
		if arg(0) == "" {
			return usage("deps-ready KEY")
		}
		var p []map[string]any
		if p, err = c.DepsReady(a[0]); err == nil {
			fmt.Println(jira.JSON(map[string]any{"key": a[0], "ready": len(p) == 0, "pending": p}, ""))
			if len(p) > 0 {
				return cli.ExitFail
			}
			return cli.ExitOK
		}
	case "upsert":
		if arg(0) == "" || arg(1) == "" {
			return usage("upsert EXTERNAL_ID JSON|FILE|-")
		}
		// the JSON may be given as text, as a file path, or as - for stdin (the `render --json` output)
		body := a[1]
		if body == "-" {
			b, _ := io.ReadAll(os.Stdin)
			body = string(b)
		} else if b, rerr := os.ReadFile(body); rerr == nil {
			body = string(b)
		}
		var key, act string
		if key, act, err = c.Upsert(a[0], body, func(m string) { fmt.Fprintln(os.Stderr, "jira: "+m) }); err == nil {
			out = map[string]any{"key": key, "externalId": a[0], "action": act}
		}
	case "comment":
		if arg(0) == "" || arg(1) == "" {
			return usage("comment KEY TEXT")
		}
		if err = c.Comment(a[0], a[1]); err == nil {
			out = map[string]any{"key": a[0], "commented": true}
		}
	case "transition":
		if arg(0) == "" || arg(1) == "" {
			return usage("transition KEY STATUS")
		}
		if err = c.Transition(a[0], a[1]); err == nil {
			out = map[string]any{"key": a[0], "transitioned": a[1]}
		}
	case "change-request":
		if arg(0) == "" || arg(1) == "" {
			return usage("change-request KEY JSON")
		}
		var k string
		if k, err = c.ChangeRequest(a[0], a[1]); err == nil {
			out = map[string]any{"key": k, "blocks": a[0], "kind": "spec-change"}
		}
	default:
		fmt.Fprintln(os.Stderr, jiraUsage)
		return cli.ExitUsage
	}
	if err != nil {
		var e *jira.Err
		code := cli.ExitContract2
		if errors.As(err, &e) {
			code = e.Code
		}
		fmt.Fprintln(os.Stderr, "jira: "+err.Error())
		return code
	}
	fmt.Println(jira.JSON(out, ""))
	return cli.ExitOK
}
