package sdd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/glob"
)

var reKey = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)=|^[ \t]*:[ \t]+"?\$\{([A-Za-z_][A-Za-z0-9_]*):=`)

// settingsFromBase makes every setting come from the base branch's committed settings file, so a pull request cannot
// loosen the gate that judges it (SDD_FREE_GLOBS=*, SDD_MODE=off, DIFF_BASE_CMD=...). A key this branch's files name is
// not read from the process environment either. Without a settings file on the base (first adoption) this branch's settings stay.
func (e *Env) settingsFromBase(base string) {
	b, err := gitx.Run(e.Root, "show", base+":"+e.kitPath()+"/"+e.P.EnvFile)
	if err != nil {
		e.printf("sdd ci-check: the base branch has no %s/%s yet (first adoption): using this branch's settings\n", e.kitPath(), e.P.EnvFile)
		return
	}
	head := map[string]bool{}
	for _, f := range []string{e.P.EnvFile, "sdkit.local.env"} {
		d, _ := os.ReadFile(filepath.Join(e.P.Dir(), f))
		for _, m := range reKey.FindAllStringSubmatch(string(d), -1) {
			head[m[1]+m[2]] = true
		}
	}
	genv := func(k string) string {
		if head[k] {
			return ""
		}
		return os.Getenv(k)
	}
	s := envfile.New()
	s.Quiet, s.Getenv = true, genv
	s.LoadBytes(e.P.EnvFile, []byte(b))
	e.Get = func(k string) string {
		if v, ok := s.Raw(k); ok && v != "" {
			return v
		}
		return genv(k)
	}
}

// CICheck is the server-side check, run in CI on the PR branch with VERIFY_BASE=origin/<target branch> from the CI job's
// environment: every change that is not only docs/specs/project files needs a committed attestation for exactly this diff;
// medium and high also need a verifier PASS. Settings come from the base branch; DIFF_BASE_CMD is never run here. It fails
// closed: an unknown base, a diff that cannot be listed or hashed, or an attestation without a real fingerprint never
// passes. With RISK_FLOOR_GLOBS (the base's, plus any this branch adds) the attestation's risk must reach the floor of the
// files the diff really touches. The attestation is a file the author commits: this proves the flow was run for this diff,
// not who ran it. Review it like code.
func (e *Env) CICheck() int {
	fail := func(f string, a ...any) int { e.printf("sdd ci-check: FAIL "+f+"\n", a...); return 1 }
	env := os.Getenv("VERIFY_BASE")
	if env == "" {
		return fail("VERIFY_BASE=origin/<target branch> must come from the CI job's environment")
	}
	head := e.Get("RISK_FLOOR_GLOBS")
	e.ciBase = env
	if gitx.RevParse(e.Root, env) == "" {
		return fail("base '%s' is not a known commit (fetch the target branch; the checkout needs the full history)", env)
	}
	e.settingsFromBase(env)
	if e.Mode() == "off" {
		e.printf("sdd ci-check: SDD_MODE=off on the base branch, skipped\n")
		return 0
	}
	if why := FloorValidate(head); why != "" {
		return fail("RISK_FLOOR_GLOBS in this branch is invalid: %s", why)
	}
	if why := FloorValidate(e.Get("RISK_FLOOR_GLOBS")); why != "" {
		return fail("RISK_FLOOR_GLOBS on the base branch is invalid: %s", why)
	}
	all := e.Get("RISK_FLOOR_GLOBS")
	if head != "" {
		all = head
		if b := e.Get("RISK_FLOOR_GLOBS"); b != "" {
			all += "," + b
		}
	}
	files, err := e.ChangedFiles()
	if err != nil {
		return fail("cannot list the changes against %s", env)
	}
	lvl, lfile := "low", ""
	if all != "" {
		lvl, lfile = FloorOf(all, files)
	}
	var gated string
	for _, f := range files {
		if !(glob.MatchList(f, e.freeGlobs(), false) && !(all != "" && floorHit(all, f))) {
			gated += " " + f
		}
	}
	if gated == "" {
		e.printf("sdd ci-check: no code changes against %s, nothing to attest\n", env)
		return 0
	}
	fp, err := e.Fingerprint()
	if err != nil {
		return fail("cannot fingerprint the diff")
	}
	reason := ""
	atts, _ := filepath.Glob(filepath.Join(e.P.Dir(), "attestations", "*.json"))
	for _, path := range atts {
		b, err := os.ReadFile(path)
		var a struct {
			Fingerprint, Risk, Verdict, Profile string
			Evidence                            map[string]struct{ Result, Trust, Fingerprint string }
		}
		if err != nil || json.Unmarshal(b, &a) != nil || a.Fingerprint != fp { // fp is 64 hex characters: an empty or broken one never matches
			continue
		}
		name, r, v := filepath.Base(path), a.Risk, a.Verdict
		if r == "" {
			r = "low"
		}
		if rank(r) < rank(lvl) {
			reason = name + ": risk " + r + " is below the risk floor " + lvl + " set by '" + lfile + "' (RISK_FLOOR_GLOBS)"
			continue
		}
		if (r != "low" || a.Profile == "high-assurance") && v != "PASS" {
			pf := a.Profile
			if pf == "" {
				pf = "standard"
			}
			reason = name + ": risk " + r + " (profile " + pf + ") needs a verifier PASS, found '" + v + "'"
			continue
		}
		if r == "high" { // each independent role has a trusted PASS for exactly this diff
			var miss []string
			for _, x := range evRoles {
				if ev := a.Evidence[x]; ev.Result != "PASS" || ev.Trust != "spawned" || ev.Fingerprint != fp {
					miss = append(miss, x)
				}
			}
			if len(miss) > 0 {
				reason = name + ": risk high needs a trusted PASS for this diff from verifier, reviewer and security-auditor; missing, blocked or untrusted: " + strings.Join(miss, ",")
				continue
			}
		}
		if v == "" {
			v = "n/a"
		}
		e.printf("sdd ci-check: ok, %s attests this diff (risk %s, verdict %s, risk floor %s)\n", name, r, v, lvl)
		return 0
	}
	if reason != "" {
		e.printf("sdd ci-check: FAIL %s\n", reason)
		if lvl != "low" {
			e.printf("  Open the task again with --risk %s or higher (the user confirms the label), verify it, and commit the new attestation.\n", lvl)
		}
		return 1
	}
	e.printf("sdd ci-check: FAIL no attestation for this diff (fingerprint %.12s). Changed:%s\n", fp, gated)
	e.printf("  Run the spec-driven-dev flow to 'verified' on this exact diff and commit %s/attestations/<task>.json.\n", e.kitPath())
	e.printf("  Editing a file after verification invalidates it: re-verify, then commit the new attestation.\n")
	return 1
}
