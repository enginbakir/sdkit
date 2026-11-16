package main

import (
	"fmt"

	"github.com/wecii/sdkit/internal/cli"
)

func init() {
	for _, n := range []string{"help", "--help", "-h"} {
		cli.Register(n, runHelp)
	}
}

func runHelp(args []string) int {
	fmt.Println("sdkit: the spec-driven-kit command\n\nCommands:")
	for _, n := range cli.Names() {
		fmt.Println("  " + n)
	}
	return cli.ExitOK
}
