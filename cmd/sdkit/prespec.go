package main

import (
	"fmt"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/setup"
)

func init() { cli.Register("prespec", runPrespec) }

func runPrespec(args []string) int {
	if len(args) != 2 || args[0] != "check" || args[1] == "" {
		fmt.Fprintln(os.Stderr, "usage: sdkit prespec check <name>")
		return cli.ExitUsage
	}
	wd, _ := os.Getwd()
	return setup.PrespecCheck(wd, args[1],
		func(s string) { fmt.Println(s) },
		func(s string) { fmt.Fprintln(os.Stderr, s) })
}
