package main

import (
	"io"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/hook"
)

func init() { cli.Register("hook", runHook) }

// runHook is `sdkit hook [--host H] <event|name>`. It never fails: exit 0 or 2 only.
func runHook(args []string) int {
	host := ""
	if len(args) >= 2 && args[0] == "--host" {
		host, args = args[1], args[2:]
	}
	if len(args) != 1 {
		return 0
	}
	in, _ := io.ReadAll(os.Stdin)
	return hook.Run(host, args[0], in, os.Stdout, os.Stderr)
}
