package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wecii/sdkit/internal/cli"
	"github.com/wecii/sdkit/internal/plan"
	"github.com/wecii/sdkit/internal/verdict"
)

// verdictRecorder is set by the sdd package (P1-07); nil records nothing.
var verdictRecorder verdict.Recorder

func init() { cli.Register("verdict", runVerdict) }

func runVerdict(args []string) int {
	if len(args) == 0 || args[0] != "parse" {
		fmt.Fprintln(os.Stderr, "usage: sdkit verdict parse [<file|->] [--proposal proposal.md] [--record] [--role verifier|reviewer|security-auditor] [--source S --run ID --task SLUG]")
		return cli.ExitUsage
	}
	args = args[1:]
	src, o := "-", verdict.Options{NoNormalize: os.Getenv("PARSE_NORMALIZE") == "0"}
	o.Accepted = func(proposal, id string) bool {
		b, err := os.ReadFile(proposal)
		if err != nil {
			return false
		}
		line := plan.Item(string(b), id)
		return line != "" && plan.Accepted(filepath.Join(filepath.Dir(proposal), "acceptances.jsonl"), id, line)
	}
	var record bool
	var source, run, task string
	if len(args) > 0 && (args[0] == "-" || args[0][0] != '-') {
		src, args = args[0], args[1:]
	}
	for i := 0; i < len(args); i++ {
		val := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--proposal":
			o.ProposalPath = val()
		case "--record":
			record = true
		case "--role":
			o.Role = val()
		case "--source":
			source = val()
		case "--run":
			run = val()
		case "--task":
			task = val()
		}
	}
	var b []byte
	var err error
	if src == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(src)
	}
	if err != nil {
		return verdict.CodeMalformed
	}
	r := verdict.ParseWith(string(b), o)
	fmt.Println(r.Line())
	if r.Kind != "" || !record || verdictRecorder == nil {
		return r.Code()
	}
	role := r.Role
	if msg, refused := verdictRecorder.Record(role, r.Verdict, source, run, task); refused {
		fmt.Printf("%s well-formed, NOT recorded: %s\n", r.Verdict, msg)
		return verdict.CodeRefused
	}
	return r.Code()
}
