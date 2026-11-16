package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/project"
)

func init() { cli.Register("pricing", runPricing) }

const pricingUsage = "usage: sdkit pricing refresh [--out FILE] [--from LIST.json] [--region R] [--regional] [--force] [--dry-run] | status [--json]"

// Exit: 0 ok, 2 usage, 3 refresh refused (hand-made table) or status found something to fix, 4 fetch failed (table unchanged).
func runPricing(args []string) int {
	if len(args) == 0 || (args[0] != "refresh" && args[0] != "status") {
		fmt.Fprintln(os.Stderr, pricingUsage)
		return cli.ExitUsage
	}
	wd, _ := os.Getwd()
	top, err := project.Root(wd)
	if err != nil {
		top = wd
	}
	s, p, _ := project.Load(top)
	file := filepath.Join(p.Dir(), "pricing.json")
	if v := os.Getenv("TELEMETRY_PRICES"); v != "" {
		file = v
	}
	if args[0] == "status" {
		js := len(args) == 2 && args[1] == "--json"
		if len(args) > 1 && !js {
			fmt.Fprintln(os.Stderr, pricingUsage)
			return cli.ExitUsage
		}
		days, _ := strconv.Atoi(os.Getenv("PRICING_STALE_DAYS"))
		st := pricing.Check(pricing.StatusOptions{File: file, WorkDir: filepath.Join(p.Dir(), "work"), BaselineModel: s.String("REPORT_BASELINE_MODEL"), StaleDays: days})
		if js {
			b, _ := json.MarshalIndent(st, "", "  ")
			fmt.Println(string(b))
		} else {
			st.Print(os.Stdout)
		}
		if len(st.Problems) > 0 {
			return cli.ExitBudget
		}
		return cli.ExitOK
	}
	o := pricing.Options{Out: file, URL: os.Getenv("SDKIT_PRICE_URL"), Now: os.Getenv("SDKIT_NOW")}
	for a := args[1:]; len(a) > 0; a = a[1:] {
		val := func() string {
			if len(a) < 2 {
				return ""
			}
			v := a[1]
			a = a[1:]
			return v
		}
		switch a[0] {
		case "--out":
			o.Out = val()
		case "--from":
			o.From = val()
		case "--region":
			o.Region = val()
		case "--regional":
			o.Regional = true
		case "--force":
			o.Force = true
		case "--dry-run":
			o.DryRun = true
		default:
			fmt.Fprintln(os.Stderr, "pricing: unknown option "+a[0])
			return cli.ExitUsage
		}
	}
	if o.Out == "" {
		fmt.Fprintln(os.Stderr, pricingUsage)
		return cli.ExitUsage
	}
	return RefreshPricing(o)
}

// RefreshPricing runs a refresh and prints the outcome; `sdkit doctor --update-pricing` calls it.
func RefreshPricing(o pricing.Options) int {
	t, err := pricing.Refresh(o)
	switch {
	case errors.Is(err, pricing.ErrRefused):
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitBudget
	case errors.Is(err, pricing.ErrFailed):
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitContract
	case err != nil:
		fmt.Fprintln(os.Stderr, "pricing:", err)
		return cli.ExitFail
	}
	if o.DryRun {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
		return cli.ExitOK
	}
	scope := "global"
	if o.Regional {
		scope = "regional"
	}
	fmt.Printf("pricing: wrote %s (%d models, %s inference)\n", o.Out, len(t.Models), scope)
	return cli.ExitOK
}
