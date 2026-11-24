package main

// sdkit design: the deterministic half of generate-design.
//
//	sdkit design acs <id>                          the spec's active acceptance criteria (what the process must account for)
//	sdkit design check <id> [--model F]            check a model file against the spec, or (no --model) the built designs/<id>/
//	sdkit design build <id> [--model F]            check the model, then write designs/<id>/ (process.json, process.bpmn, process.md, design.meta.json)
//	sdkit design publish <id> --title T --body-file F   branch design/<id> from the base, commit designs/<id>/ only, push, open the PR
//
// The model draft is <project-dir>/work/design/<id>/process.json (the flow-modeler writes it there; the phase guard allows that
// directory), and the spec is read from <specs-dir>/<id>/, which must exist: the design is made from a spec, never before it.
// Exit: 0 ok, 1 a check failed or a step was refused, 2 usage.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/design"
	"github.com/wecii/sdkit/internal/lint"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/review"
)

func init() { cli.Register("design", runDesign) }

const designUsage = "usage: sdkit design acs <id> | check <id> [--model F] | build <id> [--model F] | publish <id> --title T --body-file F"

func runDesign(args []string) int {
	if len(args) < 2 || !review.OkName(args[1]) {
		fmt.Fprintln(os.Stderr, designUsage)
		return cli.ExitUsage
	}
	verb, id, a := args[0], args[1], args[2:]
	var model, title, body string
	for i := 0; i < len(a); i++ {
		var p *string
		switch {
		case a[i] == "--model" && (verb == "check" || verb == "build"):
			p = &model
		case a[i] == "--title" && verb == "publish":
			p = &title
		case a[i] == "--body-file" && verb == "publish":
			p = &body
		}
		if p == nil || i+1 >= len(a) {
			fmt.Fprintln(os.Stderr, designUsage)
			return cli.ExitUsage
		}
		*p = a[i+1]
		i++
	}
	wd, _ := os.Getwd()
	root, err := project.Root(wd)
	if err != nil {
		root = wd
	}
	s, p, _ := project.Load(root)
	or := func(k, d string) string {
		if v := s.String(k); v != "" && !strings.HasPrefix(v, "__") {
			return v
		}
		return d
	}
	specDir := filepath.Join(root, or("SPECS_DIR", "specs"), id)
	outRel := "designs/" + id
	outDir := filepath.Join(root, filepath.FromSlash(outRel))
	fail := func(f string, a ...any) int { fmt.Fprintf(os.Stderr, "design: "+f+"\n", a...); return cli.ExitFail }

	sp, err := readSpec(specDir, id)
	if err != nil {
		return fail("%v", err)
	}
	report := func(ps []design.Problem) int {
		for _, q := range ps {
			fmt.Println(q)
		}
		if design.Errors(ps) > 0 {
			return cli.ExitFail
		}
		return cli.ExitOK
	}
	switch verb {
	case "acs":
		fmt.Printf("spec %s v%s: %d active acceptance criteria", id, sp.Version, len(sp.ACs))
		if len(sp.Withdrawn) > 0 {
			fmt.Printf(" (%d withdrawn, not listed)", len(sp.Withdrawn))
		}
		fmt.Println()
		for _, ac := range sp.ACs {
			fmt.Println(ac)
		}
		return cli.ExitOK
	case "check":
		if model == "" {
			ps := design.Verify(outDir, sp)
			if len(ps) == 0 {
				fmt.Printf("design: %s ok (spec v%s)\n", outRel, sp.Version)
			}
			return report(ps)
		}
		m, _, err := design.Load(model)
		if err != nil {
			return fail("%v", err)
		}
		ps := design.Check(m, sp)
		if len(ps) == 0 {
			fmt.Printf("design: %s ok against spec v%s\n", model, sp.Version)
		}
		return report(ps)
	case "build":
		if model == "" {
			model = filepath.Join(p.Dir(), "work", "design", id, "process.json")
		}
		m, raw, err := design.Load(model)
		if err != nil {
			return fail("%v", err)
		}
		ps := design.Check(m, sp)
		if rc := report(ps); rc != cli.ExitOK {
			return fail("the model has errors; nothing was written")
		}
		if err := design.Write(outDir, design.Render(m, raw, sp)); err != nil {
			return fail("cannot write %s: %v", outRel, err)
		}
		fmt.Printf("design: wrote %s (process.json, process.bpmn, process.md, design.meta.json) from spec v%s\n", outRel, sp.Version)
		return cli.ExitOK
	case "publish":
		if title == "" || body == "" {
			fmt.Fprintln(os.Stderr, designUsage)
			return cli.ExitUsage
		}
		if ps := design.Verify(outDir, sp); len(ps) > 0 {
			for _, q := range ps {
				fmt.Println(q)
			}
			return fail("refusing to publish: fix the problems above (a stale or hand-edited design is not published)")
		}
		get := func(k string) string {
			if v := os.Getenv(k); v != "" {
				return v
			}
			return s.String(k)
		}
		err := design.Publish(design.PublishOptions{Dir: root, Rel: outRel, ID: id, Base: or("BASE_BRANCH", "main"), Title: title, BodyFile: body,
			PRHost: get("PR_HOST"), Protected: strings.Fields(strings.ReplaceAll(s.String("PROTECTED_BRANCHES"), ",", " ")),
			Getenv: get, Out: os.Stdout, Err: os.Stderr})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return cli.ExitFail
		}
		return cli.ExitOK
	}
	fmt.Fprintln(os.Stderr, designUsage)
	return cli.ExitUsage
}

// readSpec reads the identity and the acceptance criteria of the published spec the design is made from.
func readSpec(dir, id string) (design.Spec, error) {
	sp := design.Spec{ID: id}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return sp, fmt.Errorf("no spec at %s: a design is made from a spec; run generate-specs first (or check out the spec's branch)", dir)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "spec.meta.json")); err == nil {
		var m struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(b, &m)
		sp.Version = m.Version
	}
	if sp.Version == "" {
		sp.Version = "0.0"
	}
	if sp.Hash, err = review.Hash(dir); err != nil {
		return sp, err
	}
	sp.ACs = lint.ActiveACIDs(dir)
	act := map[string]bool{}
	for _, a := range sp.ACs {
		act[a] = true
	}
	for _, a := range lint.ACIDs(dir) {
		if !act[a] {
			sp.Withdrawn = append(sp.Withdrawn, a)
		}
	}
	if len(sp.ACs) == 0 {
		return sp, fmt.Errorf("%s defines no acceptance criteria", dir)
	}
	return sp, nil
}
