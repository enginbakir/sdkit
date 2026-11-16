package main

import (
	"fmt"
	"os"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("base", runBase) }

func runBase(args []string) int {
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit: not in a git repository")
		return cli.ExitFail
	}
	s, _, _ := project.Load(root)
	get := base.Getter(s.Raw)
	b := base.Flow(root, get)
	if len(args) > 0 && args[0] == "--frozen" {
		fmt.Println(base.Frozen(root, get, b))
		return cli.ExitOK
	}
	fmt.Println(base.Resolve(root, get))
	return cli.ExitOK
}
