package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/wecii/sdkit/internal/adapter"
	"github.com/wecii/sdkit/internal/audit"
	"github.com/wecii/sdkit/internal/contract"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/redact"
	"github.com/wecii/sdkit/internal/runrole"
	"github.com/wecii/sdkit/internal/sdd"
)

// The seams the ported packages left open (run-role contracts, the egress audit of ext:litellm).
func init() {
	runrole.Seams.Contract = contractSeam{}
	adapter.Audit = func(event string, kv ...string) error {
		return audit.Log(audit.Path(project.Resolve(sdd.Root()).Dir()), event, kv)
	}
	adapter.Redact = redact.Text
	sdd.SpecApprovedSHA = specApprovedSHA
}

// specApprovedSHA runs `sdkit spec approved-sha` inside the specs checkout, as 1.x ran spec.sh there:
// the spec commands resolve the repository from the working directory.
func specApprovedSHA(dir, id, sha string, w io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(self, "spec", "approved-sha", id, "--sha", sha)
	c.Dir, c.Stdout, c.Stderr = dir, w, w
	if err := c.Run(); err != nil {
		return fmt.Errorf("spec %s@%s is not an approved revision: %w", id, sha, err)
	}
	return nil
}

// contractSeam is what run-role called as scripts/contract.sh: issue (registers the handoff), check-result --record, close.
type contractSeam struct{}

func (contractSeam) Issue(handoff, role string, checkOnly bool) (string, bool) {
	b, err := os.ReadFile(handoff) // run-role hands over the file, as contract.sh issue FILE took it
	if err != nil {
		return "cannot read the handoff: " + err.Error(), false
	}
	r := contract.New(sdd.Root()).Issue(string(b), contract.Options{Role: role, CheckOnly: checkOnly})
	return r.Line(), r.Exit() == 0
}

func (contractSeam) Result(answer, id, role, cwd string) (string, int) {
	r := contract.New(sdd.Root()).CheckResult(answer, contract.Options{WantID: id, Role: role, Cwd: cwd, Record: true})
	return r.Line(), r.Exit()
}

func (contractSeam) Close(id, reason string) { contract.New(sdd.Root()).Close(id, reason) }
