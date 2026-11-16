package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/knowledge"
	"github.com/wecii/sdkit/internal/state"
)

func init() { cli.Register("knowledge", runKnowledge) }

// runKnowledge is `sdkit knowledge put|get|list|clear <slug> [key] ...`: notes valid while the files they cover are unchanged.
func runKnowledge(args []string) int {
	use := func(s string) int { fmt.Fprintln(os.Stderr, s); return cli.ExitUsage }
	if len(args) < 2 || !state.ValidSlug(args[1]) {
		return use("usage: sdkit knowledge put|get|list|clear <slug> ...")
	}
	cmd, key := args[0], ""
	rest := args[2:]
	if (cmd == "put" || cmd == "get") && len(rest) > 0 {
		key, rest = rest[0], rest[1:]
	}
	var files, title, role string
	for i := 0; i+1 < len(rest); i += 2 {
		switch rest[i] {
		case "--files":
			files = rest[i+1]
		case "--title":
			title = rest[i+1]
		case "--role":
			role = rest[i+1]
		default:
			i--
		}
	}
	if !knowledge.ValidKey(key) {
		return use("knowledge: key may use letters, digits, . _ - only")
	}
	top, kd, ok := taskDir()
	if !ok {
		return use("sdkit knowledge needs a git repository")
	}
	k := knowledge.K{Root: top, KitDir: kd, Slug: args[1]}
	switch cmd {
	case "put":
		if key == "" || files == "" {
			return use(`usage: sdkit knowledge put <slug> <key> --files "a b"`)
		}
		b, _ := io.ReadAll(os.Stdin)
		if strings.TrimSpace(string(b)) == "" {
			return use("knowledge: empty note")
		}
		n, err := k.Put(key, title, strings.Fields(files), string(b))
		if errors.Is(err, knowledge.ErrSecret) {
			return use("knowledge: the note looks like it holds a secret; not stored")
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "knowledge:", err)
			return cli.ExitFail
		}
		fmt.Printf("stored %s (covers %d file(s))\n", key, n)
	case "get":
		s, err := k.Get(key, role)
		if err != nil {
			_, msg, _ := strings.Cut(err.Error(), ": ")
			fmt.Fprintln(os.Stderr, "knowledge:", msg)
			if errors.Is(err, knowledge.ErrRole) {
				return cli.ExitContract2
			}
			return cli.ExitFail
		}
		fmt.Print(s)
	case "list":
		for _, l := range k.List() {
			fmt.Println(l)
		}
	case "clear":
		k.Clear()
		fmt.Println("cleared")
	default:
		return use("usage: sdkit knowledge put|get|list|clear <slug> ...")
	}
	return cli.ExitOK
}
