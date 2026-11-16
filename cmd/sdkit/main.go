// Command sdkit is the spec-driven-kit CLI.
package main

import (
	"os"

	"github.com/wecii/sdkit/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
