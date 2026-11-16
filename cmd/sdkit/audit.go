package main

import (
	"fmt"
	"os"
	"strconv"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/audit"
	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() { cli.Register("audit", runAudit) }

const auditUsage = "usage: sdkit audit log <type> [key=value ...] | show [--type T] [-n N] | verify | policy"

func runAudit(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, auditUsage)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	path := audit.Path(project.Resolve(sdd.Root()).Dir())
	switch cmd {
	case "log":
		typ := ""
		if len(args) > 0 {
			typ, args = args[0], args[1:]
		}
		if !audit.ValidType(typ) {
			fmt.Fprintln(os.Stderr, "audit: type must match [a-z.]+")
			return cli.ExitUsage
		}
		if err := audit.Log(path, typ, args); err != nil {
			fmt.Fprintln(os.Stderr, "audit:", err)
			return cli.ExitUsage
		}
	case "show":
		typ, n := "", 20
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--type", "-n":
				if i+1 >= len(args) {
					return cli.ExitUsage
				}
				if i++; args[i-1] == "--type" {
					typ = args[i]
				} else if v, err := strconv.Atoi(args[i]); err == nil {
					n = v
				}
			}
		}
		lines, err := audit.Show(path, typ, n)
		if err != nil {
			fmt.Println("audit: no entries")
			return cli.ExitOK
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	case "verify":
		n, broken, err := audit.Verify(path)
		switch {
		case err != nil:
			fmt.Println("audit: no entries (ok)")
		case broken > 0:
			fmt.Printf("audit: chain broken at line %d\n", broken)
			return cli.ExitFail
		default:
			fmt.Printf("audit: chain ok (%d entries)\n", n)
		}
	case "policy":
		bad := audit.Policy(kit.FS)
		for _, b := range bad {
			fmt.Println("FAIL: " + b)
		}
		if len(bad) > 0 {
			return cli.ExitFail
		}
		fmt.Println("audit policy: ok")
	default:
		fmt.Fprintln(os.Stderr, auditUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}
