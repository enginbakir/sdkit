package main

import (
	"os"
	"time"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/memory"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("memory", runMemory) }

func runMemory(args []string) int {
	cwd, _ := os.Getwd()
	top, err := gitx.Top(cwd)
	if err != nil {
		top = cwd
	}
	set, p, _ := project.Load(top)
	m := &memory.Memory{Root: top, KitDir: p.KitDir, Get: base.Getter(set.Raw), In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Now: time.Now()}
	return m.Run(args)
}
