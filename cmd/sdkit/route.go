package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/recover"
	"github.com/wecii/sdkit/internal/route"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() {
	cli.Register("route", runRoute)
	reservationsLister = func() (items []recover.Item) { // the stale budget reservations: old AND the owning process is gone
		l, _ := route.New(sdd.Root()).Reservations()
		for _, x := range l {
			if x.State == "stale" {
				items = append(items, recover.Item{Kind: "reservation", ID: x.Slug + "/" + x.ID, Action: "sdkit route reservations expire",
					Detail: fmt.Sprintf("reserved USD held %ds with no outcome and no live process", x.AgeS), Applicable: true})
			}
		}
		return items
	}
}

const routeUsage = `usage: sdkit route <command>   (no model call; ROUTING=on makes run-role a consumer of the decisions)
  decide  --role R [--host H] [--tier T] [--model M] [--handoff FILE] [--risk L] [--rerun WHY] [--ignore-budget] [--require-cli] [--reserve] [--pid N] [--json]
  explain --role R ...      the same, as readable lines; never records anything
  outcome --decision ID --rc N [--prc N] [--stderr FILE] [--dur-ms N] [--cost-est USD] [--in N --out N] [--tokens actual|unavailable]
  classify --rc N [--prc N] [--stderr FILE]   the failure class of an exit status
  registry [--json] | capabilities [host] [--json|--probe] | validate | log [slug] | reservations [list|expire] [--json]
decide exit: 0 run it | 7 reuse: valid evidence exists | 8 stop, nothing valid exists | 3 budget exhausted | 2 refused by policy or configuration`

func jsonOut(v any) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	fmt.Print(b.String())
}

func table(rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	w.Flush()
}

func runRoute(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, routeUsage)
		return cli.ExitUsage
	}
	cmd, args := args[0], args[1:]
	r := route.New(sdd.Root())
	flagErr := func(f string, a ...any) int { fmt.Fprintf(os.Stderr, f+"\n", a...); return cli.ExitUsage }
	switch cmd {
	case "decide", "explain":
		var q route.Query
		for i := 0; i < len(args); i++ {
			next := func() string {
				if i+1 < len(args) {
					i++
					return args[i]
				}
				return ""
			}
			switch a := args[i]; a {
			case "--role":
				q.Role = next()
			case "--host":
				q.Host = next()
			case "--tier":
				q.Tier = next()
			case "--model":
				q.Model = next()
			case "--handoff":
				q.Handoff = next()
			case "--risk":
				q.Risk = next()
			case "--rerun":
				q.Rerun = next()
			case "--source":
				q.Source = next()
			case "--pid":
				p, err := strconv.Atoi(next())
				if err != nil || p < 0 {
					return flagErr("--pid must be a number")
				}
				q.PID = p
			case "--ignore-budget":
				q.IgnoreBudget = true
			case "--require-cli":
				q.RequireCLI = true
			case "--reserve":
				q.Reserve = true
			case "--json":
			default:
				return flagErr("unknown option %s", a)
			}
		}
		if q.Role == "" {
			return flagErr("--role is required")
		}
		q.Reserve = q.Reserve && cmd == "decide"
		d, err := r.Decide(q)
		if err != nil {
			return flagErr("route: %v", err)
		}
		if cmd == "explain" {
			fmt.Println(d.Explain())
		} else {
			b, _ := json.Marshal(d)
			fmt.Println(string(b))
		}
		return d.Exit
	case "outcome":
		var id string
		o := route.Outcome{Tokens: "unavailable"}
		bad := false
		atoi := func(s string) int { n, err := strconv.Atoi(s); bad = bad || err != nil; return n }
		for i := 0; i+1 < len(args); i += 2 {
			v := args[i+1]
			switch args[i] {
			case "--decision":
				id = v
			case "--rc":
				o.RC = atoi(v)
			case "--prc":
				o.PRC = atoi(v)
			case "--stderr":
				b, _ := os.ReadFile(v)
				o.Stderr = string(b)
			case "--dur-ms":
				n := int64(atoi(v))
				o.DurMS = &n
			case "--cost-est":
				o.CostEst = v
			case "--in":
				o.In = atoi(v)
			case "--out":
				o.Out = atoi(v)
			case "--tokens":
				o.Tokens = v
			default:
				return flagErr("unknown option %s", args[i])
			}
		}
		if id == "" || bad || !containsFlag(args, "--rc") {
			return flagErr("outcome needs --decision ID and --rc N")
		}
		cls, err := r.Close(id, o)
		if err != nil {
			return flagErr("%v", err)
		}
		fmt.Println(cls)
	case "classify":
		var o route.Outcome
		have := false
		for i := 0; i+1 < len(args); i += 2 {
			switch args[i] {
			case "--rc":
				n, err := strconv.Atoi(args[i+1])
				o.RC, have = n, err == nil
			case "--prc":
				o.PRC, _ = strconv.Atoi(args[i+1])
			case "--stderr":
				b, _ := os.ReadFile(args[i+1])
				o.Stderr = string(b)
			}
		}
		if !have {
			return flagErr("classify needs --rc N")
		}
		fmt.Println(route.Classify(o.RC, o.PRC, o.Stderr))
	case "validate":
		if errs := r.Validate(); len(errs) > 0 {
			fmt.Fprintln(os.Stderr, strings.Join(errs, "\n"))
			return cli.ExitUsage
		}
		fmt.Printf("routing configuration ok (ROUTING=%s, policy %s/%s)\n", orElse(r.Get("ROUTING"), "off"), route.Policy, orElse(r.Get("ROUTE_POLICY"), "tiered"))
	case "log":
		slug := r.Slug()
		if len(args) > 0 {
			slug = args[0]
		}
		l := r.Log(slug)
		if len(l) == 0 {
			fmt.Printf("no routing decisions recorded for %s\n", slug)
		}
		for _, s := range l {
			fmt.Println(s)
		}
	case "registry":
		reg, err := r.Registry()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitUsage
		}
		if len(args) > 0 && args[0] == "--json" {
			jsonOut(reg)
			break
		}
		var rows [][]string
		for _, m := range reg {
			roles := "*"
			if l, ok := m.Roles.([]any); ok {
				roles = fmt.Sprint(strings.Trim(fmt.Sprint(l), "[]"))
				roles = strings.ReplaceAll(roles, " ", ",")
			}
			cost := "cost=unknown"
			if m.Cost != nil {
				cost = "cost=set"
			}
			rows = append(rows, []string{m.ID, m.Adapter, orElse(*pstr(m.Model), "(host default)"), orElse(*pstr(m.Effort), "-"), m.Tier, roles, m.Status, "usage=" + m.UsageAccounting, cost, m.Source})
		}
		table(rows)
	case "capabilities":
		return routeCaps(r, args)
	case "reservations":
		act, js := "list", false
		for _, a := range args {
			switch a {
			case "list", "expire":
				act = a
			case "--json":
				js = true
			default:
				return flagErr("usage: sdkit route reservations [list|expire] [--json]")
			}
		}
		if act == "expire" {
			n, err := r.Expire()
			if err != nil {
				return flagErr("reservations: a reservations.jsonl cannot be read (restore it; nothing is guessed)")
			}
			fmt.Printf("reservations: expired %d stale reservation(s)\n", n)
			break
		}
		l, err := r.Reservations()
		if err != nil {
			return flagErr("reservations: a reservations.jsonl cannot be read (restore it; nothing is guessed)")
		}
		switch {
		case js:
			if l == nil {
				l = []route.Reservation{}
			}
			jsonOut(l)
		case len(l) == 0:
			fmt.Println("no open reservations")
		default:
			for _, x := range l {
				fmt.Printf("%s\t%s\t$%s\tage %ds\t%s\n", x.Slug, x.ID, strconv.FormatFloat(x.USD, 'f', -1, 64), x.AgeS, x.State)
			}
		}
	default:
		fmt.Fprintln(os.Stderr, routeUsage)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

func containsFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func orElse(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func pstr(p *string) *string {
	if p == nil {
		e := ""
		return &e
	}
	return p
}

// routeCaps is `capabilities [host] [--json|--probe]`. --probe reads only `<cli> --help` and `--version` of the CLIs installed here: no model call.
func routeCaps(r *route.Router, args []string) int {
	host, js, probe := "", false, false
	for _, a := range args {
		switch a {
		case "--json":
			js = true
		case "--probe":
			probe = true
		default:
			host = a
		}
	}
	ab, _ := route.KitAdapters()
	ad, err := route.LoadAdapters(ab)
	if err != nil {
		fmt.Fprintln(os.Stderr, "adapters.json is missing")
		return cli.ExitUsage
	}
	names := ad.Order
	if host != "" {
		names = []string{host}
	}
	if probe {
		for _, h := range names {
			a, ok := ad.M[h]
			if !ok {
				continue
			}
			if _, err := exec.LookPath(a.CLI); err != nil {
				fmt.Printf("%s: %s not installed here: every capability stays as declared in adapters.json (unverified)\n", h, a.CLI)
				continue
			}
			v, _ := proc.Run(context.Background(), "", nil, a.CLI, "--version")
			ver, _, _ := strings.Cut(strings.TrimSpace(v.Stdout+v.Stderr), "\n")
			hr, _ := proc.Run(context.Background(), "", nil, a.CLI, append(append([]string{}, a.Probe.Help...), "--help")...)
			res := ""
			for _, cap := range route.Keys(rawField(ad.Raw[h], "probe", "flags")) {
				if a.Caps[cap] == "none" {
					continue
				}
				st := "help-verified"
				for _, f := range a.Probe.Flags[cap] {
					if !regexp.MustCompile(`(^|[\s,\[])` + regexp.QuoteMeta(f) + `([\s,=<\[]|$)`).MatchString(hr.Stdout + hr.Stderr) {
						st = "MISSING:" + f
						break
					}
				}
				res += " " + cap + "=" + st
			}
			fmt.Printf("%s (%s):%s\n", h, ver, res)
		}
		return cli.ExitOK
	}
	if js {
		var b bytes.Buffer
		b.WriteString("{")
		for i, h := range names {
			if i > 0 {
				b.WriteString(",")
			}
			k, _ := json.Marshal(h)
			b.Write(k)
			b.WriteString(":")
			b.Write(ad.Raw[h])
		}
		b.WriteString("}")
		var out bytes.Buffer
		_ = json.Indent(&out, b.Bytes(), "", "  ")
		fmt.Println(out.String())
		return cli.ExitOK
	}
	var rows [][]string
	for _, h := range names {
		if _, ok := ad.M[h]; !ok {
			continue
		}
		var kv []string
		for _, k := range route.Keys(rawField(ad.Raw[h], "caps")) {
			kv = append(kv, k+"="+ad.M[h].Caps[k])
		}
		rows = append(rows, []string{h, strings.Join(kv, " ")})
	}
	table(rows)
	fmt.Println("(help-verified = flags present in the local --help, behaviour not exercised; declared = documented headless mode, never checked here; estimated = host's own client-side estimate, not billing; kit = provided by the kit; none = unsupported; see --probe)")
	return cli.ExitOK
}

func rawField(raw json.RawMessage, path ...string) json.RawMessage {
	for _, p := range path {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
		raw = m[p]
	}
	return raw
}
