package design

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/pr"
)

// PublishOptions is one `sdkit design publish`. Dir is the repository root, Rel the design directory relative to it
// (designs/<id>); Getenv resolves environment and sdkit.env values for the PR host.
type PublishOptions struct {
	Dir, Rel, ID, Base, Title, BodyFile, PRHost string
	Protected                                   []string
	Getenv                                      func(string) string
	Out, Err                                    io.Writer
}

// publishBranch is where a design goes. Right after `generate-specs` the session is on the spec's own branch, whose pull request is
// still open: the design joins that one pull request, so the Tech Lead reviews spec and design together and nobody opens two. On any
// other branch (the spec is merged, or the design is made alone) it has a branch of its own, cut from the base, and its own pull request.
func publishBranch(cur, id string) (branch string, withSpec bool) {
	if cur == "spec/"+id {
		return cur, true
	}
	return "design/" + id, false
}

// Publish commits only the design directory and pushes it, either onto the spec's branch (and so into the spec's pull request) or on
// its own branch `design/<id>` with a pull request of its own (see publishBranch). The spec's files and its content hash are never
// touched. The push happens here, not in the agent's shell, because the phase guard refuses an agent's own `git push` while no
// spec-driven task is open.
func Publish(o PublishOptions) error {
	g := func(a ...string) (string, error) { return gitx.Run(o.Dir, a...) }
	ok := func(a ...string) bool { _, err := g(a...); return err == nil }
	say := func(f string, a ...any) { fmt.Fprintf(o.Out, f+"\n", a...) }
	die := func(f string, a ...any) error { return fmt.Errorf("design: "+f, a...) }

	cur, _ := g("rev-parse", "--abbrev-ref", "HEAD")
	b, withSpec := publishBranch(cur, o.ID)
	for _, p := range append([]string{o.Base, "main", "master", "develop", "trunk"}, o.Protected...) {
		if b == p {
			return die("branch %s is protected", b)
		}
	}
	_ = ok("fetch", "-q", "origin")
	if cur != b {
		switch {
		case ok("show-ref", "--verify", "-q", "refs/heads/"+b):
			if !ok("checkout", "-q", b) {
				return die("cannot switch to %s (commit or stash your changes; nothing is forced)", b)
			}
		case ok("show-ref", "--verify", "-q", "refs/remotes/origin/"+b):
			if !ok("checkout", "-q", "-b", b, "--track", "origin/"+b) {
				return die("cannot switch to %s (commit or stash your changes; nothing is forced)", b)
			}
		default:
			from := "origin/" + o.Base
			if !ok("rev-parse", "--verify", "-q", from) {
				from = o.Base
			}
			// the untracked design directory comes along; the spec's own files stay on its branch
			if !ok("checkout", "-q", "-b", b, from) {
				return die("cannot cut %s from %s: commit or stash your tracked changes first (nothing is forced); an existing %s on %s is a clash to resolve by hand", b, from, o.Rel, from)
			}
		}
	}
	if !ok("add", "--", o.Rel) {
		return die("git add failed")
	}
	if !ok("diff", "--cached", "--quiet", "--", o.Rel) {
		if !ok("commit", "-q", "-m", fmt.Sprintf("docs(design): %s process flow", o.ID), "--", o.Rel) {
			return die("commit failed")
		}
	}
	if err := gitx.RunInteractive(o.Dir, os.Stdin, "push", "-q", "-u", "origin", b); err != nil {
		var xe *gitx.ExitError
		if errors.As(err, &xe) && xe.Stderr != "" {
			fmt.Fprintf(o.Err, "design: git push said:\n%s\n", xe.Stderr)
		}
		return die("push failed; fix the remote and run publish again (the commit stays on %s)", b)
	}
	var buf bytes.Buffer
	r := &pr.Request{Dir: o.Dir, Mode: "create", Title: o.Title, DescFile: o.BodyFile, Dest: o.Base, Reuse: true, Getenv: o.Getenv, Out: &buf, Err: o.Err, Host: o.PRHost}
	if err := r.Run(context.Background()); err != nil {
		return die("pushed %s, but opening the pull request failed (%v): fix the cause and run publish again (it only opens the PR)", b, err)
	}
	l := gitx.Lines(buf.String())
	url := ""
	if len(l) > 0 {
		url = strings.TrimSpace(l[len(l)-1])
	}
	if withSpec {
		say("design: added '%s' to the spec's pull request on %s -> %s", o.ID, b, url)
		return nil
	}
	say("design: published '%s' on %s -> %s", o.ID, b, url)
	return nil
}
