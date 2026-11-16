package main

import (
	"os"

	"github.com/wecii/sdkit/internal/adapter"
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/runrole"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() {
	cli.Register("run-role", func(args []string) int { return runrole.Run(args, os.Stdout, os.Stderr) })
	cli.Register("adapter", func(args []string) int {
		root := sdd.Root()
		return adapter.Finder{ProjectDir: project.Resolve(root).Dir()}.Run(args, os.Stdout, os.Stderr)
	})
}
