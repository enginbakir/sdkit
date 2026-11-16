package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/verify"
)

func init() { cli.Register("verify", runVerify) }

// runVerify: sdkit verify [--fast] [--since-head] [--no-cache]
func runVerify(args []string) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitFail
	}
	return verify.Run(context.Background(), dir, verify.Parse(args), os.Stdout)
}
