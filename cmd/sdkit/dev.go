package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/devscan"
	"github.com/wecii/sdkit/internal/emit"
	"github.com/wecii/sdkit/internal/gitx"
)

func init() { cli.Register("dev", runDev) }

const devUsage = "usage: sdkit dev adapters [--check] | sdkit dev scan [--history]"

// kitRoot finds the kit checkout (the directory with core/roles) from the cwd upward.
func kitRoot() (string, error) {
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, e := os.Stat(filepath.Join(d, "core", "roles")); e == nil && st.IsDir() {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			return "", fmt.Errorf("core/roles not found: run this inside the kit checkout")
		}
		d = p
	}
}

func runDev(args []string) int {
	if len(args) > 0 && args[0] == "scan" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--history") {
			fmt.Fprintln(os.Stderr, devUsage)
			return cli.ExitUsage
		}
		cwd, _ := os.Getwd()
		root, err := gitx.Top(cwd)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sdkit:", err)
			return 2
		}
		return devscan.Run(root, len(args) == 2, os.Stdout)
	}
	if len(args) == 0 || args[0] != "adapters" || len(args) > 2 || (len(args) == 2 && args[1] != "--check") {
		fmt.Fprintln(os.Stderr, devUsage)
		return cli.ExitUsage
	}
	root, err := kitRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitFail
	}
	if len(args) == 2 {
		drift, n, err := emit.Check(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sdkit:", err)
			return cli.ExitFail
		}
		for _, l := range drift {
			fmt.Println(l)
		}
		if len(drift) > 0 {
			return cli.ExitFail
		}
		fmt.Printf("adapters up to date (%d role files)\n", n)
		return cli.ExitOK
	}
	n, err := emit.Write(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitFail
	}
	fmt.Printf("built %d role files: agents/ (Claude Code), adapters/{codex,opencode,cursor,gemini}/\n", n)
	return cli.ExitOK
}
