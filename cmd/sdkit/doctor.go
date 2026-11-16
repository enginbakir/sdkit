package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/setup"
)

func init() { cli.Register("doctor", runDoctor) }

func runDoctor(args []string) int {
	dir, update := "", false
	for _, a := range args {
		switch {
		case a == "--update-pricing":
			update = true
		case dir == "" && a != "" && a[0] != '-':
			dir = a
		default:
			fmt.Fprintln(os.Stderr, "usage: sdkit doctor [--update-pricing] [target-dir]")
			return cli.ExitUsage
		}
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkit doctor:", err)
		return cli.ExitUsage
	}
	if update {
		file := filepath.Join(root, "pricing.json")
		if _, p, err := project.Load(root); err == nil {
			file = filepath.Join(p.Dir(), "pricing.json")
		}
		if v := os.Getenv("TELEMETRY_PRICES"); v != "" {
			file = v
		}
		if rc := RefreshPricing(pricing.Options{Out: file, URL: os.Getenv("SDKIT_PRICE_URL"), Now: os.Getenv("SDKIT_NOW")}); rc != cli.ExitOK {
			// as 1.x: a failed refresh is a warning, the old table stays and doctor carries on
			fmt.Printf("  warn  pricing not updated (refresh exit %d)\n", rc)
		}
	}
	if fails, _ := setup.Doctor(root, setup.New(root), os.Getenv); fails > 0 {
		return cli.ExitFail
	}
	return cli.ExitOK
}
