package main

import (
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/report"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() {
	cli.Register("report", func(args []string) int { return report.New(sdd.Root()).Run(args) })
	sdd.Rep = report.Rep{}
}
