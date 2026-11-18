package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/ui"
)

// sdkit ui [--no-open] [--host <cmd>]   start the local panel for this repo (docs/design-ui.md); prints the address
// sdkit ui ask [--json <questions>]      inside a panel session: ask the person, wait for the answer, print it as JSON
//
//	(questions come from --json or stdin, in the shape of the host's question tool: {"questions":[{"question","options":[{"label"}]}]})
func init() { cli.Register("ui", runUI) }

const uiUsage = "usage: sdkit ui [--no-open] [--host <cmd>] | sdkit ui ask [--json <questions>]"

// uiRules is added to every task: the person is not technical and there is no terminal to ask on.
const uiRules = `You are running inside the sdkit panel, for a person who is not a developer and cannot see a terminal.
- Never use the AskUserQuestion tool. To ask the person anything, run: sdkit ui ask --json '<questions>' (same JSON shape as AskUserQuestion's input: {"questions":[{"question":"...","header":"...","multiSelect":false,"options":[{"label":"...","description":"..."}]}]}). It waits for the answer and prints it as JSON; read it and continue.
- Write for a non-technical reader: short, plain, no command names or file paths in what you say to them. Keep printing the step banners the skill asks for.
- Do not stop to report between steps; stop only when the skill says a person must act.

Task: `

func runUI(args []string) int {
	if len(args) > 0 && args[0] == "ask" {
		return runUIAsk(args[1:])
	}
	open, host := true, ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-open":
			open = false
		case "--host":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, uiUsage)
				return cli.ExitUsage
			}
			i++
			host = args[i]
		default:
			fmt.Fprintln(os.Stderr, uiUsage)
			return cli.ExitUsage
		}
	}
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		root = wd
	}
	env := []string{}
	if exe, err := os.Executable(); err == nil {
		// the session calls `sdkit ...` by name; make it find this binary even when it is not on PATH
		env = append(env, "PATH="+filepath.Dir(exe)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cfg := ui.Config{
		Dir:     root,
		Command: host,
		Tools:   []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep", "Task", "Skill"},
		Env:     env,
		Prompt:  func(task string, _ []string) string { return uiRules + task },
	}
	srv := ui.New(cfg, ui.NewSecret())
	l, err := srv.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit ui:", err)
		return cli.ExitFail
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Println("sdkit ui: " + srv.URL())
	fmt.Println("Ctrl+C to stop.")
	if open {
		openBrowser(srv.URL())
	}
	if err := srv.Serve(ctx, l); err != nil {
		fmt.Fprintln(os.Stderr, "sdkit ui:", err)
		return cli.ExitFail
	}
	return cli.ExitOK
}

func runUIAsk(args []string) int {
	var q []byte
	if len(args) == 2 && args[0] == "--json" {
		q = []byte(args[1])
	} else if len(args) == 0 {
		q, _ = io.ReadAll(os.Stdin)
	} else {
		fmt.Fprintln(os.Stderr, uiUsage)
		return cli.ExitUsage
	}
	ans, err := ui.Ask(context.Background(), os.Getenv("SDKIT_UI_URL"), os.Getenv("SDKIT_UI_KEY"), q)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit ui ask:", err)
		return cli.ExitFail
	}
	fmt.Println(string(ans))
	return cli.ExitOK
}

// openBrowser is best effort: the address is already printed.
func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	c.Start()
}
