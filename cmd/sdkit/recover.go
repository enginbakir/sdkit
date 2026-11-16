package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/recover"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/state"
)

func init() { cli.Register("recover", runRecover) }

// reservationsLister is set by the router (P4): the stale budget reservations as recover items.
var reservationsLister func() []recover.Item

// runRecover is `sdkit recover scan|apply [slug] [--json]`; exit 0 nothing to do / done, 1 scan found items, 2 usage.
func runRecover(args []string) int {
	usage := func() int {
		fmt.Fprintln(os.Stderr, "usage: sdkit recover scan|apply [slug] [--json]")
		return cli.ExitUsage
	}
	if len(args) == 0 || args[0] != "scan" && args[0] != "apply" {
		return usage()
	}
	mode, slug, js := args[0], "", false
	for _, a := range args[1:] {
		switch {
		case a == "--json":
			js = true
		case len(a) > 0 && a[0] == '-':
			fmt.Fprintf(os.Stderr, "unknown option %s\n", a)
			return cli.ExitUsage
		default:
			slug = a
		}
	}
	e := sdd.New(sdd.Root())
	if slug == "" {
		slug = state.ActiveSlug(e.P)
	}
	notask := slug == ""
	if notask {
		slug = "_none"
	}
	ttl := 1800
	if v, err := strconv.Atoi(e.Get("RECOVER_STALE_S")); err == nil && v >= 0 {
		ttl = v
	}
	sc := recover.Scanner{P: e.P, Slug: slug, TTL: time.Duration(ttl) * time.Second, Now: time.Now(), Reservations: reservationsLister}
	items := sc.Scan()
	if notask && len(items) == 0 {
		fmt.Println("recover: no open task")
		return cli.ExitOK
	}
	if mode == "scan" {
		switch {
		case js:
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			_ = enc.Encode(items)
			fmt.Print(b.String())
		case len(items) == 0:
			fmt.Printf("recover: nothing interrupted or stale for %s\n", slug)
		default:
			for _, it := range items {
				how := "manual"
				if it.Applicable {
					how = "apply"
				}
				fmt.Printf("%s\t%s\t%s\t%s\n    -> %s\n", it.Kind, it.ID, how, it.Detail, it.Action)
			}
		}
		if len(items) > 0 {
			return cli.ExitFail
		}
		return cli.ExitOK
	}
	closed, manual := sc.Apply(items, func(it recover.Item) int { return closeItem(e, slug, it) })
	fmt.Printf("recover: closed %d stale item(s); %d need a manual step (sdkit recover scan)\n", closed, manual)
	return cli.ExitOK
}

// closeItem closes one item through the command that owns it; a command that is not built yet counts as a failure (rc 127).
func closeItem(e *sdd.Env, slug string, it recover.Item) int {
	id := it.ID
	switch it.Kind {
	case "spawn":
		if strings.Contains(it.Action, " --run ") {
			return e.SpawnClose([]string{"--run", id, "--reason", "interrupted"})
		}
		return e.SpawnClose([]string{"--role", id, "--source", "hook", "--reason", "interrupted"})
	case "contract":
		return viaCommand("contract", "close", id, "--reason", "interrupted", "--source", "recover")
	case "dispatch":
		return viaCommand("dispatch", "release", id, "--slug", slug)
	case "reservation":
		return viaCommand("route", "reservations", "expire")
	}
	return 0
}

func viaCommand(args ...string) int {
	for _, n := range cli.Names() {
		if n == args[0] {
			so, se := os.Stdout, os.Stderr
			if null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
				os.Stdout, os.Stderr = null, null
				defer func() { os.Stdout, os.Stderr = so, se; null.Close() }()
			}
			return cli.Run(args)
		}
	}
	return 127
}
