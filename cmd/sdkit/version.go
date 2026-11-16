package main

import (
	"encoding/json"
	"fmt"
	"os"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/cli"
)

func init() {
	for _, n := range []string{"version", "--version", "-v"} {
		cli.Register(n, runVersion)
	}
}

func runVersion(args []string) int {
	b, err := kit.FS.ReadFile(".claude-plugin/plugin.json")
	var p struct {
		Version string `json:"version"`
	}
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit: cannot read embedded plugin.json:", err)
		return cli.ExitFail
	}
	fmt.Println("sdkit " + p.Version)
	return cli.ExitOK
}
