package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/setup"
)

func init() { cli.Register("upgrade", runUpgrade) }

// upgradeSelf runs another subcommand of this binary and returns its output.
func upgradeSelf(args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(exe, args...).CombinedOutput()
	return string(out), err
}

func runUpgrade(args []string) int {
	root, _ := os.Getwd()
	for _, a := range args {
		if a == "--migrate" || a == "--apply" {
			fmt.Fprintln(os.Stderr, "sdkit upgrade --migrate was dropped in 2.0: run the last 1.x release's upgrade --migrate first")
			return cli.ExitUsage
		}
		p, err := filepath.Abs(a)
		if err != nil || a[0] == '-' {
			fmt.Fprintln(os.Stderr, "usage: sdkit upgrade [dir]")
			return cli.ExitUsage
		}
		root = p
	}
	return setup.New(root).Upgrade(upgradeSelf)
}
