package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/digest"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/runrole"
	"github.com/wecii/sdkit/internal/state"
)

func init() {
	cli.Register("digest", runDigest)
	runrole.Seams.Envelope = func(answer, role, ref string) string { return newDigest("").Envelope(answer, role, ref) }
}

// newDigest reads the project of the current directory; slug "" means the active task.
func newDigest(slug string) digest.D {
	cwd, _ := os.Getwd()
	top := os.Getenv("CLAUDE_PROJECT_DIR")
	if top == "" {
		if t, err := gitx.Top(cwd); err == nil {
			top = t
		} else {
			top = cwd
		}
	}
	set, p, _ := project.Load(top)
	if slug == "" {
		slug = state.ActiveSlug(p)
	}
	min := 1500
	if v := os.Getenv("ENVELOPE_MIN_BYTES"); v != "" {
		min, _ = strconv.Atoi(v)
	} else if set != nil {
		min = set.Int("ENVELOPE_MIN_BYTES", 1500)
	}
	return digest.D{Dir: p.Dir(), Slug: slug, MinBytes: min, Stats: os.Getenv("DIGEST_STATS") != "0"}
}

// runDigest is `sdkit digest output|envelope|stats`.
func runDigest(args []string) int {
	usage := func(s string) int { fmt.Fprintln(os.Stderr, s); return cli.ExitUsage }
	if len(args) == 0 {
		return usage("usage: sdkit digest output|envelope|stats ...")
	}
	mode, max, ref, role, file, label, slug := args[0], 60, "", "", "", "out", ""
	for i := 1; i < len(args); i++ {
		a, val := args[i], ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch a {
		case "--max":
			max, _ = strconv.Atoi(val)
		case "--ref":
			ref = val
		case "--role":
			role = val
		case "--file":
			file = val
		case "--label":
			label = val
		default:
			if mode != "stats" {
				return usage("digest: unknown option " + a)
			}
			slug = a
			continue
		}
		i++
	}
	d := newDigest(slug)
	in := func() string {
		var b []byte
		if file != "" {
			b, _ = os.ReadFile(file)
		} else {
			b, _ = io.ReadAll(os.Stdin)
		}
		return string(b)
	}
	switch mode {
	case "output":
		fmt.Print(d.Output(in(), max, ref, label))
	case "envelope":
		if role == "" {
			return usage("usage: sdkit digest envelope --role R [--file F] --ref FILE")
		}
		t := in()
		fmt.Print(d.Envelope(t, role, ref))
		if len(t) > d.MinBytes {
			fmt.Println()
		}
	case "stats":
		fmt.Print(d.Summary())
	default:
		return usage("usage: sdkit digest output|envelope|stats ...")
	}
	return cli.ExitOK
}
