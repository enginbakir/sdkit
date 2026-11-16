package setup

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wecii/sdkit/internal/store"
)

// UpgradeRun runs another sdkit subcommand (init, doctor) and returns its output; nil skips those steps.
type UpgradeRun func(args ...string) (string, error)

// upgradeLegacy reports a pre-1.0 layout (D2: not migrated by 2.0).
func upgradeLegacy(root string) bool {
	for _, p := range []string{".agentkit", ".claude/project.env", ".codex/project.env"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			return true
		}
	}
	return false
}

// upgradeRetired is true for files 1.x installed that 2.0 no longer has: copied scripts and hooks, or
// anything whose kit source is gone.
func (s *Setup) upgradeRetired(e syncEntry) bool {
	for _, p := range []string{s.KitDir + "/scripts/", s.KitDir + "/hooks/", s.KitDir + "/adapters.d/", s.KitDir + "/roles/", ".claude/hooks/"} {
		if strings.HasPrefix(e.dest, p) {
			return true
		}
	}
	for _, f := range []string{s.KitDir + "/tiers.env", s.KitDir + "/adapters.json"} {
		if e.dest == f {
			return true
		}
	}
	_, err := fs.Stat(s.FS, e.src)
	return err != nil
}

// upgradeRemove deletes retired kit-owned files that the user did not edit and reports edited ones.
// It works from the manifest as it was before this upgrade (old): step 1 runs init, which rewrites the
// manifest without the 1.x entries. Both kinds are then dropped from the current manifest. Files the old
// manifest does not list are never touched.
func (s *Setup) upgradeRemove(old []syncEntry) error {
	retired := map[string]bool{}
	for _, e := range old {
		if !s.upgradeRetired(e) {
			continue
		}
		retired[e.dest] = true
		p := s.abs(e.dest)
		switch _, err := os.Stat(p); {
		case err != nil:
		case shaFile(p) != e.sha:
			s.say("  kept    %s (retired in 2.0 but edited; remove it yourself)", e.dest)
		default:
			if err := os.Remove(p); err != nil {
				return err
			}
			_ = os.Remove(filepath.Dir(p)) // only when empty
			s.say("  removed %s", e.dest)
		}
	}
	if len(retired) == 0 {
		s.say("  nothing to remove")
		return nil
	}
	var keep []string
	for _, e := range s.syncReadManifest() {
		if !retired[e.dest] {
			keep = append(keep, e.dest+"\t"+e.src+"\t"+e.sha)
		}
	}
	return store.WriteAtomic(s.syncManifest(), []byte(strings.Join(keep, "\n")+"\n"))
}

func upgradeVer(v string) (n [3]int) {
	for i, f := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		n[i], _ = strconv.Atoi(strings.TrimRight(f, "-+abcdefghijklmnopqrstuvwxyz"))
	}
	return
}

var upgradeTel = regexp.MustCompile(`(?m)^TELEMETRY=(""|'')?$`)
var upgradeRoute = regexp.MustCompile(`(?m)^ROUTING="?off"?$`)

// Upgrade brings a 1.x project up to this kit version: it adds new files (through sdkit init), removes
// what 2.0 no longer has, syncs the managed copies and runs doctor. It returns the exit code.
func (s *Setup) Upgrade(run UpgradeRun) int {
	dir := filepath.Join(s.Root, ResolveKit(s.Root))
	if k := ResolveKit(s.Root); k == "" {
		if upgradeLegacy(s.Root) {
			s.say("pre-1.0 layout found: run the last 1.x release's `upgrade --migrate --apply` first, then upgrade again")
		} else {
			s.say("no sdkit.env in .sdkit/: this repo is not set up (run sdkit init)")
		}
		return 1
	} else {
		s.KitDir = k
	}
	vb, _ := os.ReadFile(filepath.Join(dir, ".scaffold-version"))
	from := strings.TrimSpace(string(vb))
	if from == "" {
		from = "unknown"
	}
	hosts, mode := DetectHosts(s.Root), DetectMode(s.Root)
	st, persona := s.stack(), ""
	eb, _ := os.ReadFile(filepath.Join(dir, s.KitEnv))
	for _, l := range splitLines(string(eb)) {
		if v, ok := strings.CutPrefix(l, "STACK="); ok {
			st = strings.Trim(v, "\"' ")
		} else if v, ok := strings.CutPrefix(l, "SDKIT_ENV="); ok {
			persona = strings.Trim(v, "\"' ")
		}
	}
	s.say("upgrade: %s (project dir %s): kit %s -> %s", s.Root, s.KitDir, from, s.Version)
	s.say("detected: host=%s mode=%s stack=%s", strings.Join(hosts, ","), mode, st)

	old := s.syncReadManifest() // before init rewrites it (step 3 needs the 1.x entries)
	s.say("\n1. new files (existing files are kept)")
	if run != nil {
		args := []string{"init", "--host", strings.Join(hosts, ","), "--mode", mode, "--stack", st, "--dir", s.KitDir}
		if persona != "" {
			args = append(args, "--env", persona)
		}
		out, _ := run(append(args, s.Root)...)
		n := 0
		for _, l := range splitLines(out) {
			if strings.HasPrefix(l, "  wrote") {
				s.say("%s", l)
				n++
			}
		}
		if n == 0 {
			s.say("  nothing new")
		}
	}

	if from == "unknown" || upgradeLess(upgradeVer(from), [3]int{1, 3, 2}) {
		s.say("\n1b. measurement defaults (kit %s -> 1.3.2: telemetry and routing on)", from)
		for _, f := range []string{filepath.Join(dir, "sdkit.local.env"), filepath.Join(dir, s.KitEnv)} {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			t := upgradeTel.ReplaceAllString(string(b), "TELEMETRY=1")
			t = upgradeRoute.ReplaceAllString(t, "ROUTING=on")
			if t != string(b) {
				_ = os.WriteFile(f, []byte(t), 0o644)
				s.say("  set    %s: telemetry/routing defaults turned on", filepath.Base(f))
			}
		}
	}

	s.say("\n2. .gitignore lines")
	if gi, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
		tpl, _ := s.kitFile("templates/sdkit.gitignore")
		have, out, added := map[string]bool{}, string(gi), false
		for _, l := range splitLines(out) {
			have[l] = true
		}
		for _, l := range splitLines(tpl) {
			if l == "" || l[0] == '#' || l[0] == '!' || have[l] {
				continue
			}
			if !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += l + "\n"
			s.say("  added  %s", l)
			added = true
		}
		if added {
			_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(out), 0o644)
		} else {
			s.say("  up to date")
		}
	}

	s.say("\n3. retired in 2.0 (kit-owned, unedited files only)")
	if err := s.upgradeRemove(old); err != nil {
		s.say("  FAIL   %v", err)
		return 1
	}

	s.say("\n4. managed copies (sdkit sync)")
	if _, err := s.Sync(false); err != nil {
		s.say("  FAIL   %v", err)
		return 1
	}

	s.say("\n5. what to do by hand")
	s.say("  see docs/upgrading.md (sections for versions after %s)", from)
	s.say("  each clone: git config core.hooksPath .githooks (the git hooks call sdkit, which must be on PATH)")
	if mode == "plugin" {
		s.say("  plugin mode: also refresh the plugin in Claude Code (/plugin, update spec-driven-kit).")
	}
	if run != nil {
		out, _ := run("doctor", s.Root)
		s.say("\n%s", strings.TrimRight(out, "\n"))
	}
	_ = put(filepath.Join(dir, ".scaffold-version"), []byte(s.Version+"\n"), 0o644)
	return 0
}

func upgradeLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
