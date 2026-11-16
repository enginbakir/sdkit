package main

import (
	"io"
	"os"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/redact"
)

func init() { cli.Register("redact", runRedact) }

// runRedact is `sdkit redact`: stdin to stdout with credential-shaped strings replaced by [REDACTED:<kind>].
func runRedact(args []string) int {
	b, _ := io.ReadAll(os.Stdin)
	os.Stdout.WriteString(redact.Text(string(b)))
	return cli.ExitOK
}
