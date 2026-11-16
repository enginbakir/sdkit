package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/sdd"
)

func init() { Register(SessionStart, nil, "session-context", sessionContext) }

var ticketRe = regexp.MustCompile(`[A-Z]+-[0-9]+`)

// sessionContext gives the agent branch, ticket and open-change context.
func sessionContext(in *Input) Decision {
	root := in.Root
	get := func(k string) string { return setting(in, k) }
	or := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	branch, _ := gitx.Run(root, "rev-parse", "--abbrev-ref", "HEAD")
	ticket := ticketRe.FindString(branch)
	// listing every open change floods the context, so only the one matching the branch ticket is named
	var changes []string
	if es, err := os.ReadDir(filepath.Join(root, "openspec", "changes")); err == nil && ticket != "" {
		for _, e := range es {
			if strings.Contains(strings.ToLower(e.Name()), strings.ToLower(ticket)) {
				changes = append(changes, e.Name())
			}
		}
	}
	integ := or(get("BASE_BRANCH"), "develop")
	msg := "Branch: " + or(branch, "unknown") + ". Integration branch: " + integ + "."
	if get("BASE_BRANCH_MODE") == "release" {
		ref := base.Resolve(root, get)
		if strings.Contains(ref, "/"+or(get("RELEASE_BRANCH_PREFIX"), "release/")) {
			state := base.Frozen(root, get, ref)
			msg += " Active release: " + ref + " (" + state + "). Cut new work from it, merge to " + integ + " for test, never bring " + integ + " into a feature branch."
			if state == "frozen" {
				msg += " The release is FROZEN: no new work may be added to it."
			}
		} else {
			msg += " No release branch found locally (run: git fetch --tags origin); falling back to " + ref + "."
		}
	}
	if ticket != "" {
		msg += " Ticket from branch: " + ticket + "."
	}
	if len(changes) > 0 {
		msg += " Matching openspec change: " + strings.Join(changes, " ")
	}
	e := sdd.New(root)
	var out bytes.Buffer
	e.Out, e.Err = &out, &out
	e.Status()
	status := or(strings.TrimSpace(out.String()), "no active task")
	msg += " Workflow: " + status + " (SDD_MODE=" + e.Mode() + "); code edits, push and PR are gated by phase; start with the spec-driven-dev skill."
	msg += " Verify with the spec-driven-kit verify script"
	if v := get("VERIFY_CMD"); v != "" {
		msg += " (" + v + ")"
	}
	return Decision{Context: msg + "."}
}
