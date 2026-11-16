package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
)

func init() { cli.Register("where", runWhere) }

func runWhere(args []string) int {
	exe, err := os.Executable()
	if err == nil {
		if r, e := filepath.EvalSymlinks(exe); e == nil {
			exe = r
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit:", err)
		return cli.ExitFail
	}
	fmt.Println(filepath.Dir(exe))
	return cli.ExitOK
}
