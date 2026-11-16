package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/setup"
	"github.com/wecii/sdkit/internal/stack"
)

func init() { cli.Register("init", runInit) }

const initUsage = "usage: sdkit init [--install-openspec] [--ci bitbucket|github|gitlab] [--host claude[,codex,opencode,cursor,gemini]] [--dir .sdkit] [--mode plugin|copy] [--stack NAME] [--env standalone|analyst|developer] [--no-memory] [--memory-vcs commit|ignore] [--force] [--marketplace-url URL] [target-dir]"

func runInit(args []string) int {
	usage := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "sdkit init: "+format+"\n", a...)
		return cli.ExitUsage
	}
	root, _ := os.Getwd()
	o, force, url := setup.InitOptions{Hosts: []string{"claude"}, Mode: "plugin"}, false, ""
	val := func(i *int) string {
		*i++
		if *i < len(args) {
			return args[*i]
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--host":
			o.Hosts = strings.Split(val(&i), ",")
		case "--dir":
			if d := val(&i); d != ".sdkit" {
				return usage("--dir must be .sdkit (pre-1.0 directory names are not supported in 2.0)")
			}
		case "--mode":
			o.Mode = val(&i)
		case "--stack":
			o.Stack = val(&i)
		case "--env":
			o.Env = val(&i)
		case "--ci":
			o.CI = val(&i)
		case "--memory-vcs":
			o.MemoryVCS = val(&i)
		case "--marketplace-url":
			url = val(&i)
		case "--force":
			force = true
		case "--install-openspec":
			o.InstallOpenSpec = true
		case "--no-memory":
			o.NoMemory = true
		case "--memory":
			o.NoMemory = false
		default:
			p, err := filepath.Abs(a)
			if strings.HasPrefix(a, "-") || err != nil {
				return usage("unknown option %s\n%s", a, initUsage)
			}
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				return usage("%s is not a directory", a)
			}
			root = p
		}
	}
	for _, c := range []struct {
		name, v string
		ok      []string
	}{
		{"--ci", o.CI, []string{"", "bitbucket", "github", "gitlab"}},
		{"--memory-vcs", o.MemoryVCS, []string{"", "commit", "ignore"}},
		{"--env", o.Env, []string{"", "standalone", "analyst", "developer"}},
		{"--mode", o.Mode, []string{"plugin", "copy"}},
		{"--stack", o.Stack, append([]string{""}, stack.Names()...)},
	} {
		if !contains(c.ok, c.v) {
			return usage("%s takes %s, got '%s'", c.name, strings.TrimLeft(strings.Join(c.ok, ", "), ", "), c.v)
		}
	}
	for _, h := range o.Hosts {
		if !contains([]string{"claude", "codex", "opencode", "cursor", "gemini"}, h) {
			return usage("--host takes claude, codex, opencode, cursor and/or gemini, got '%s'", h)
		}
	}
	s := setup.New(root)
	s.Force, s.MarketplaceURL = force, url
	return s.Init(o)
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
