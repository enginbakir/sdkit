package sdd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/store"
)

// Base is the ref the diff is measured against: the base a CI job gave, else DIFF_BASE_CMD, else the branch flow.
func (e *Env) Base() string {
	if e.ciBase != "" {
		return e.ciBase
	}
	return base.Resolve(e.Root, e.Get)
}

// ChangedFiles lists the files that differ from where this branch left the diff base (tracked or not), sorted, each once.
// The base moving on does not change the list. The workflow state and the attestations are not part of the change they
// describe. It fails when git cannot answer (unknown base, shallow clone): callers fail closed.
func (e *Env) ChangedFiles() ([]string, error) {
	b := e.Base()
	if gitx.RevParse(e.Root, b) == "" {
		return nil, fmt.Errorf("unknown base '%s'", b)
	}
	mb, err := gitx.MergeBase(e.Root, b, "HEAD")
	if err != nil || mb == "" {
		mb = b
	}
	kd := e.kitPath()
	// audit.jsonl grows with every egress call (Jira, litellm); counting it would make evidence stale on each call (Q4).
	spec := []string{"--", ".", ":!" + kd + "/.sdd", ":!" + kd + "/attestations", ":!" + kd + "/audit.jsonl"}
	diff, err := gitx.Run(e.Root, append([]string{"diff", "-z", "--name-only", "--no-renames", mb}, spec...)...)
	if err != nil {
		return nil, err
	}
	untracked, err := gitx.Run(e.Root, append([]string{"ls-files", "-z", "-o", "--exclude-standard"}, spec...)...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range append(strings.Split(diff, "\x00"), strings.Split(untracked, "\x00")...) {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	sort.Strings(files) // byte order, as `sort -z` in the C locale
	return files, nil
}

// git runs git in the repository with stdin and returns trimmed stdout.
func (e *Env) git(stdin string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir, c.Stdin = e.Root, strings.NewReader(stdin)
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	return strings.TrimSpace(out.String()), err
}

// entry is what the fingerprint records for one changed path: "<mode> <object id>", or "deleted".
func (e *Env) entry(f string) (string, error) {
	p := filepath.Join(e.Root, f)
	fi, err := os.Lstat(p)
	switch {
	case os.IsNotExist(err):
		return "deleted", nil
	case err != nil:
		return "", err
	case fi.Mode()&os.ModeSymlink != 0:
		t, err := os.Readlink(p)
		if err != nil {
			return "", err
		}
		id, err := e.git(t, "hash-object", "--stdin")
		return "120000 " + id, err
	case fi.IsDir(): // a submodule: its checked-out commit, or the recorded one when it is not checked out (CI)
		var id string
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			id, err = e.git("", "-C", p, "rev-parse", "-q", "--verify", "HEAD")
			if err != nil {
				return "", err
			}
		} else {
			out, _ := e.git("", "ls-files", "-s", "--", f)
			for _, l := range strings.Split(out, "\n") {
				if c := strings.Fields(l); len(c) > 1 && c[0] == "160000" {
					id = c[1]
					break
				}
			}
		}
		if id == "" {
			return "", fmt.Errorf("no commit for %s", f)
		}
		return "160000 " + id, nil
	case fi.Mode().IsRegular():
		id, err := e.git("", "hash-object", "--", f)
		if err != nil {
			return "", err
		}
		if fi.Mode()&0o111 != 0 {
			return "100755 " + id, nil
		}
		return "100644 " + id, nil
	}
	return "", fmt.Errorf("not a file, link or directory: %s", f)
}

// Fingerprint is the SHA-256 of every changed path's mode and content: "<path>\0<mode> <id>\n" for each path in byte order.
// It is stable across commit and rebase onto a moved base and changes on any further edit, chmod or submodule bump. It
// fails, and an empty fingerprint never matches anything, when the diff cannot be listed or hashed.
func (e *Env) Fingerprint() (string, error) {
	files, err := e.ChangedFiles()
	if err != nil {
		return "", fmt.Errorf("cannot list the changes against '%s' (unknown ref? fetch it; CI needs the full history)", e.Base())
	}
	h := sha256.New()
	for _, f := range files {
		en, err := e.entry(f)
		if err != nil {
			return "", fmt.Errorf("cannot fingerprint '%s'", f)
		}
		fmt.Fprintf(h, "%s\x00%s\n", f, en)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkFresh: the verified diff is the diff that exists now (and the task got that far).
func (e *Env) checkFresh(t *Task) bool {
	if index(t.Phase) < index("verified") || t.VerifiedFingerprint == "" {
		return false
	}
	fp, err := e.Fingerprint()
	return err == nil && fp == t.VerifiedFingerprint
}

// CheckFresh is `check-fresh`: exit 0 only when the verified diff is the diff that exists now.
func (e *Env) CheckFresh() int {
	t := e.load()
	if t == nil {
		return e.noTask()
	}
	if !e.checkFresh(t) {
		return 1
	}
	return 0
}

// Attestation is the committed proof that a diff was verified. CI cannot see the git-ignored state; it checks this file.
type Attestation struct {
	Task        string            `json:"task"`
	Risk        string            `json:"risk"`
	Profile     string            `json:"profile"`
	Floor       string            `json:"floor"`
	Verdict     string            `json:"verdict"`
	Evidence    map[string]Attest `json:"evidence"`
	Fingerprint string            `json:"fingerprint"`
	Base        string            `json:"base"`
	Kit         string            `json:"kit"`
	At          string            `json:"at"`
	Ticket      string            `json:"ticket,omitempty"`
	SpecID      string            `json:"specId,omitempty"`
	SpecSha     string            `json:"specSha,omitempty"`
}

// Attest is one role's evidence as an attestation records it.
type Attest struct {
	Result      string `json:"result"`
	Trust       string `json:"trust"`
	Fingerprint string `json:"fingerprint"`
}

func (e *Env) kitVersion() string {
	if b, err := kit.FS.ReadFile(".claude-plugin/plugin.json"); err == nil {
		var p struct{ Version string }
		if json.Unmarshal(b, &p) == nil && p.Version != "" {
			return p.Version
		}
	}
	if b, err := os.ReadFile(filepath.Join(e.P.Dir(), ".scaffold-version")); err == nil {
		return strings.TrimSpace(string(b))
	}
	return "unknown"
}

// writeAttestation records that fp was verified. It is written only here; it vouches for the repo's own rules, so protect
// <project-dir>/attestations/ with code review (CODEOWNERS).
func (e *Env) writeAttestation(fp string) {
	t := e.load()
	if t == nil {
		return
	}
	floor := "low"
	if g := e.Get("RISK_FLOOR_GLOBS"); g != "" && FloorValidate(g) == "" {
		files, _ := e.ChangedFiles()
		floor, _ = FloorOf(g, files)
	}
	a := Attestation{Task: t.Task, Risk: t.Risk, Profile: t.Profile, Floor: floor, Evidence: map[string]Attest{}, Fingerprint: fp, Base: e.Base(),
		Kit: e.kitVersion(), At: now(), Ticket: t.Ticket}
	if a.Profile == "" {
		a.Profile = "standard"
	}
	if t.Verdict != nil {
		a.Verdict = t.Verdict.Result
	}
	for r, v := range t.Evidence {
		a.Evidence[r] = Attest{v.Result, v.Trust, v.Fingerprint}
	}
	if a.Ticket != "" { // managed tasks only: a standalone attestation is unchanged
		a.SpecID, a.SpecSha = t.SpecID, t.SpecSha
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	if err := store.WriteAtomic(filepath.Join(e.P.Dir(), "attestations", t.Task+".json"), append(b, '\n')); err != nil {
		e.warnf("sdd: cannot write the attestation: %v\n", err)
		return
	}
	e.printf("attestation written: %s/attestations/%s.json (commit it with the change)\n", e.kitPath(), t.Task)
}
