package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/sdd"
	"github.com/wecii/sdkit/internal/stack"
)

// doctor is one read-only health check of a repo's kit setup.
type doctor struct {
	s          *Setup
	getenv     func(string) string
	fails, wrn int
}

func (d *doctor) ok(f string, a ...any) { d.s.say("  ok    "+f, a...) }
func (d *doctor) bad(f string, a ...any) {
	d.fails++
	d.s.say("  FAIL  "+f, a...)
}
func (d *doctor) warn(f string, a ...any) {
	d.wrn++
	d.s.say("  warn  "+f, a...)
}
func (d *doctor) test(c bool, okMsg string, bad func()) {
	if c {
		d.ok("%s", okMsg)
	} else {
		bad()
	}
}

func (d *doctor) has(rel string) bool { _, err := os.Stat(d.s.abs(rel)); return err == nil }
func (d *doctor) isDir(rel string) bool {
	st, err := os.Stat(d.s.abs(rel))
	return err == nil && st.IsDir()
}
func (d *doctor) glob(pat string) bool { m, _ := filepath.Glob(d.s.abs(pat)); return len(m) > 0 }

func doctorValidJSON(p string) bool {
	b, err := os.ReadFile(p)
	return err == nil && json.Valid(b)
}

func doctorTool(root, name string) bool {
	if _, err := proc.LookPath(name); err == nil {
		return true
	}
	for _, ext := range []string{"", ".cmd"} {
		if _, err := os.Stat(filepath.Join(root, "node_modules", ".bin", name+ext)); err == nil {
			return true
		}
	}
	return false
}

var doctorKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*=`)

// doctorMissing lists the KEY= settings of template tpl that file lacks (colon forms count only when colon is set).
func doctorMissing(file, tpl string, colon bool) (miss []string) {
	have := map[string]bool{}
	fb, _ := os.ReadFile(file)
	for _, l := range splitLines(string(fb)) {
		if k := strings.TrimSuffix(doctorKey.FindString(l), "="); k != "" {
			have[k] = true
		} else if k := settingKey(l); colon && k != "" {
			have[k] = true
		}
	}
	tb, _ := fs.ReadFile(kit.FS, tpl)
	seen := map[string]bool{}
	for _, l := range splitLines(string(tb)) {
		if k := strings.TrimSuffix(doctorKey.FindString(l), "="); k != "" && !have[k] && !seen[k] {
			seen[k] = true
			miss = append(miss, k)
		}
	}
	sort.Strings(miss)
	return
}

var doctorTodo = regexp.MustCompile("(^|[^`])TODO($|[^`])")

// Doctor prints what is missing in the kit setup of root and returns the FAIL and warning counts.
// It reads files and runs read-only git commands; it never writes. getenv is os.Getenv in production.
func Doctor(root string, s *Setup, getenv func(string) string) (fails, warns int) {
	d := &doctor{s: s, getenv: getenv}
	s.Root = root
	s.say("doctor: %s", root)
	d.test(gitx.RevParse(root, "HEAD") != "" || func() bool { _, err := gitx.Run(root, "rev-parse", "--git-dir"); return err == nil }(),
		"git repository", func() { d.bad("not a git repository") })
	if kd := ResolveKit(root); kd != "" {
		s.KitDir = kd
	}
	dir := s.KitDir
	d.ok("project directory: %s", dir)
	envp := "standalone"
	cfg := envfile.New()
	envPath := filepath.Join(root, dir, s.KitEnv)
	var env *envfile.Settings
	if d.has(dir + "/" + s.KitEnv) {
		env = cfg
		var ign bytes.Buffer
		env.Quiet, env.Warn = false, &ign
		_ = env.LoadFile(envPath)
		_ = env.LoadFile(filepath.Join(root, dir, "sdkit.local.env"))
		if ign.Len() == 0 {
			d.ok("%s parses", s.KitEnv)
		} else {
			d.bad("settings lines the kit ignores (shell code in a settings file is not run; see docs/upgrading.md, \"Settings files are parsed\"):")
			for _, l := range splitLines(ign.String()) {
				s.say("          %s", l)
			}
		}
		envp = d.settings(env, dir)
	} else {
		d.bad("no sdkit.env in .sdkit/: run sdkit init")
	}
	if env != nil {
		d.measurement(env, dir)
	}
	d.projectFiles(dir, envp)
	d.claudeSettings()
	hosts := d.hosts(dir)
	d.versions(dir)
	d.ciGate()
	d.cliNotes(hosts)
	if env != nil {
		d.tools(env, envp)
		if envp == "analyst" { // only generate-specs uses OpenSpec; spec-driven-dev does not
			d.openspec(hosts)
		}
		if envp != "standalone" || env.String("TRACKER") == "jira" {
			d.checklist(env, dir)
		}
	}
	s.say("")
	s.say("doctor: %d failure(s), %d warning(s)", d.fails, d.wrn)
	return d.fails, d.wrn
}

// settings runs the sdkit.env checks and returns the install persona.
func (d *doctor) settings(env *envfile.Settings, dir string) string {
	s, root := d.s, d.s.Root
	envp := env.String("SDKIT_ENV")
	if envp == "" {
		envp = "standalone"
	}
	switch envp {
	case "standalone":
	case "analyst", "developer":
		d.ok("install env: %s", envp)
	default:
		d.bad("SDKIT_ENV '%s' is not standalone, analyst or developer", envp)
		envp = "standalone"
	}
	if st := env.String("STACK"); envp != "analyst" && st == "" {
		d.warn("STACK is not set in %s: the kit assumes '%s'. Set STACK=<%s>", s.KitEnv, DefaultStack, strings.Join(stack.Names(), "|"))
	}
	base := env.String("BASE_BRANCH")
	d.test(gitx.RevParse(root, base) != "" || gitx.RevParse(root, "origin/"+base) != "", fmt.Sprintf("BASE_BRANCH '%s' exists", base),
		func() { d.bad("BASE_BRANCH '%s' is not a branch in this repo", base) })
	if v := env.String("VERIFY_CMD"); envp == "analyst" {
	} else if v != "" {
		st, err := os.Stat(filepath.Join(root, v))
		d.test(err == nil && st.Mode().IsRegular() && (st.Mode().Perm()&0o111 != 0 || winOS()), "VERIFY_CMD executable",
			func() { d.bad("VERIFY_CMD '%s' missing or not executable", v) })
	} else {
		d.ok("VERIFY_CMD empty: the built-in verify engine is used")
	}
	if g := env.String("RISK_FLOOR_GLOBS"); g != "" {
		why := sdd.FloorValidate(g)
		d.test(why == "", "RISK_FLOOR_GLOBS is well-formed", func() {
			d.bad("RISK_FLOOR_GLOBS is invalid: %s (level:glob,level:glob with level low|medium|high; the workflow refuses to start and CI fails until it is fixed)", why)
		})
	}
	mp := env.String("MAX_PARALLEL")
	if mp == "" {
		mp = "3"
	}
	if n := env.Int("MAX_PARALLEL", 3); strings.Trim(mp, "0123456789") != "" || n <= 0 {
		d.bad("MAX_PARALLEL '%s' is not a positive integer", mp)
	} else {
		d.ok("MAX_PARALLEL=%s (most subagents at once)", mp)
	}
	if env.String("MEMORY") == "1" {
		d.ok("project memory on")
	} else {
		d.ok("project memory off (sdkit init --memory, or MEMORY=1 in sdkit.local.env)")
	}
	loc := filepath.Join(root, dir, "sdkit.local.env")
	miss := doctorMissing(filepath.Join(root, dir, s.KitEnv), "templates/sdkit.env", true)
	miss = append(miss, doctorMissing(loc, "templates/sdkit.local.env", false)...)
	if len(miss) == 0 {
		d.ok("settings files have every setting of this kit version")
	} else {
		d.warn("settings lacking: %s (defaults apply; sdkit upgrade adds them)", strings.Join(miss, " "))
	}
	if gitx.IsIgnored(root, dir+"/"+s.KitEnv) {
		d.warn("%s/%s is git-ignored but it holds the team's settings: add '!%s' to %s/.gitignore (sdkit upgrade does)", dir, s.KitEnv, s.KitEnv, dir)
	} else {
		d.ok("%s is shared through git", s.KitEnv)
	}
	if !d.has(dir + "/sdkit.local.env") {
		d.warn("no %s/sdkit.local.env: personal settings (limits, memory behaviour, hosts); sdkit upgrade creates it", dir)
	}
	if gitx.Tracked(root, dir+"/sdkit.local.env") {
		d.warn("%s/sdkit.local.env is committed but it is personal: git rm --cached %s/sdkit.local.env", dir, dir)
	} else {
		d.ok("sdkit.local.env stays out of git")
	}
	switch envp {
	case "developer":
		sd := env.String("SPECS_DIR")
		if sd == "" {
			sd = "specs"
		}
		if _, err := gitx.Run(filepath.Join(root, sd), "rev-parse", "--git-dir"); d.has(sd+"/.git") && err == nil {
			d.ok("%s/ submodule present", sd)
		} else {
			d.warn("%s/ submodule missing: git submodule add %s %s (then git submodule update --init)", sd, orDefault(env.String("SPEC_REPO"), "<SPEC_REPO>"), sd)
		}
		d.test(env.String("SDD_MANAGED") == "on", "SDD_MANAGED=on", func() { d.warn("SDD_MANAGED is not on (developer env runs managed)") })
		d.test(env.String("TRACKER") == "jira", "TRACKER=jira", func() {
			d.warn("TRACKER is not jira (set TRACKER=jira in %s; developer env takes tasks from Jira)", s.KitEnv)
		})
	case "analyst":
		pd := strings.TrimSuffix(orDefault(env.String("PRESPEC_DIR"), "prespec"), "/")
		if gitx.IsIgnored(root, pd+"/x") {
			d.ok("%s/ is git-ignored (pre-specs are never committed)", pd)
		} else {
			d.bad("%s/ is not git-ignored: add /%s/ to .gitignore (sdkit init --env analyst does)", pd, pd)
		}
		if out, _ := gitx.Run(root, "ls-files", "--", pd); out != "" {
			d.bad("files under %s/ are tracked: git rm -r --cached %s", pd, pd)
		}
	}
	if m := env.String("BASE_BRANCH_MODE"); m != "" {
		d.ok("release-branch flow on (BASE_BRANCH_MODE=%s)", m)
	}
	return envp
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func winOS() bool { return filepath.Separator == '\\' }

// measurement: the Value Report is only as good as what is recorded; role tier overrides.
func (d *doctor) measurement(env *envfile.Settings, dir string) {
	var miss []string
	for _, kv := range [][2]string{{"USAGE_LOG", "1"}, {"TELEMETRY", "1"}, {"VALUE_REPORT", "on"}, {"ROUTING", "on"}} {
		if v, _ := env.Raw(kv[0]); v != kv[1] { // set in the files, not just defaulted
			miss = append(miss, kv[0]+"="+kv[1])
		}
	}
	if len(miss) == 0 {
		d.ok("measurement on (USAGE_LOG, TELEMETRY, VALUE_REPORT, ROUTING)")
	} else {
		d.warn("measurement off, so the Value Report has gaps (tokens, phases, routing): %s (USAGE_LOG, TELEMETRY, VALUE_REPORT in sdkit.local.env; ROUTING in sdkit.env)", strings.Join(miss, " "))
	}
	m := env.Map()
	var keys []string
	for k := range m {
		if strings.HasPrefix(k, "AGENT_") && strings.HasSuffix(k, "_TIER") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	rank := map[string]int{"light": 1, "standard": 2, "strong": 3}
	for _, k := range keys {
		r := strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(k, "AGENT_"), "_TIER"), "_", "-"))
		v := m[k]
		b, err := fs.ReadFile(kit.FS, "core/roles/"+r+".md")
		if err != nil {
			b, err = os.ReadFile(filepath.Join(d.s.Root, dir, "roles", r+".md"))
		}
		if err != nil {
			var names []string
			ents, _ := fs.ReadDir(kit.FS, "core/roles")
			for _, e := range ents {
				if n := strings.TrimSuffix(e.Name(), ".md"); n != e.Name() && n != "README" {
					names = append(names, n)
				}
			}
			d.warn("%s: there is no role '%s' (roles: %s)", k, r, strings.Join(names, " "))
			continue
		}
		if v == "" {
			continue
		}
		if rank[v] == 0 {
			d.warn("%s=%s is not a tier (light | standard | strong): the role keeps its own tier", k, v)
			continue
		}
		own := ""
		if mm := regexp.MustCompile(`(?m)^tier:\s*(\S+)`).FindSubmatch(b); mm != nil {
			own = string(mm[1])
		}
		if r == "verifier" || r == "reviewer" || r == "security-auditor" {
			if rank[v] < rank[own] {
				d.warn("%s=%s is below the role's own tier (%s): independent roles are never moved down, the value is ignored", k, v, own)
			}
		}
	}
	// price table and routing baseline
	file := filepath.Join(d.s.Root, dir, "pricing.json")
	if v := d.getenv("TELEMETRY_PRICES"); v != "" {
		file = v
	}
	days, _ := strconv.Atoi(d.getenv("PRICING_STALE_DAYS"))
	st := pricing.Check(pricing.StatusOptions{File: file, WorkDir: filepath.Join(d.s.Root, dir, "work"), BaselineModel: env.String("REPORT_BASELINE_MODEL"), StaleDays: days})
	var pb bytes.Buffer
	st.Print(&pb)
	lines := splitLines(pb.String())
	if len(st.Problems) == 0 {
		d.ok("%s", lines[0])
		return
	}
	d.warn("%s", lines[0])
	for _, l := range lines[1:] {
		d.s.say("        %s", strings.TrimPrefix(l, "  - "))
	}
	d.s.say("        fix: set REPORT_BASELINE_MODEL=<model id from pricing.json> in sdkit.env; refresh the table with: sdkit doctor --update-pricing")
}

func (d *doctor) projectFiles(dir, envp string) {
	root := d.s.Root
	if b, err := os.ReadFile(filepath.Join(root, dir, "project.md")); err == nil {
		d.test(!doctorTodo.Match(b), "project.md filled in", func() { d.bad("project.md still has TODO placeholders") })
		d.test(bytes.Contains(b, []byte("`high`")), "risk rubric present", func() { d.bad("project.md has no risk rubric") })
	} else {
		d.bad("%s/project.md missing", dir)
	}
	for _, f := range []string{"known-pitfalls.md", ".gitignore"} {
		d.test(d.has(dir+"/"+f), fmt.Sprintf("%s/%s present", dir, f), func() { d.bad("%s/%s missing", dir, f) })
	}
	if d.has(".claude/settings.json") || d.has(".claude/settings.local.json") {
		d.test(d.has(".claude/CLAUDE.md"), ".claude/CLAUDE.md present", func() { d.bad(".claude/CLAUDE.md missing") })
	}
	if envp != "analyst" {
		d.test(d.glob(dir+"/verify.d/[0-9]*.sh"), "verify.d has active policies",
			func() { d.warn("no active verify.d policy (rename .example files to enable the ones you want)") })
	}
}

func jsonPath(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func (d *doctor) claudeSettings() {
	name := "settings.json"
	if d.has(".claude/settings.local.json") {
		name = "settings.local.json"
	}
	p := d.s.abs(".claude/" + name)
	b, err := os.ReadFile(p)
	if err != nil {
		if d.has("AGENTS.md") {
			d.ok("no .claude settings: setup for non-Claude hosts only (AGENTS.md present)")
		} else {
			d.bad(".claude/settings.local.json (or settings.json) missing: run sdkit init")
		}
		return
	}
	var doc any
	if json.Unmarshal(b, &doc) != nil {
		d.bad("%s is not valid JSON", name)
		return
	}
	d.ok("%s valid", name)
	deny, _ := jsonPath(doc, "permissions", "deny").([]any)
	d.test(len(deny) > 0, "permissions.deny set", func() { d.warn("no permissions.deny list") })
	if DetectMode(d.s.Root) == "copy" {
		// 2.0 copy mode wires `sdkit hook <event>`; .claude/hooks/*.sh are 1.x leftovers.
		if ents, _ := filepath.Glob(d.s.abs(".claude/hooks/*.sh")); len(ents) > 0 {
			d.warn("%d 1.x hook script(s) left in .claude/hooks: run sdkit upgrade", len(ents))
		}
		d.test(bytes.Contains(b, []byte("sdkit hook")), "copy mode: sdkit hook wired in "+name, func() { d.bad("copy mode: %s does not wire sdkit hook (run sdkit sync)", name) })
		return
	}
	d.test(jsonPath(doc, "enabledPlugins", "spec-driven-kit@spec-driven-kit") == true, "plugin mode: spec-driven-kit enabled in "+name,
		func() { d.bad("%s does not enable spec-driven-kit", name) })
	url, _ := jsonPath(doc, "extraKnownMarketplaces", "spec-driven-kit", "source", "url").(string)
	if url == "" || strings.Contains(url, "invalid") {
		d.bad("marketplace url missing in %s (extraKnownMarketplaces)", name)
	} else {
		d.ok("marketplace: %s", url)
	}
}

// hosts checks the non-Claude host files; it returns the hosts that need their CLI noted.
func (d *doctor) hosts(dir string) []string {
	root := d.s.Root
	b, _ := os.ReadFile(d.s.abs("AGENTS.md"))
	agents := bytes.Contains(b, []byte("spec-driven-kit:begin")) || d.isDir(".agents/skills/spec-driven-dev")
	var hs []string
	if d.has(".claude/settings.json") || d.has(".claude/settings.local.json") {
		hs = append(hs, "claude")
	}
	if d.has(".codex/config.toml") || d.isDir(".codex/agents") {
		hs = append(hs, "codex")
	}
	for _, h := range []string{"opencode", "cursor", "gemini"} {
		if d.isDir("." + h) {
			hs = append(hs, h)
		}
	}
	if agents {
		d.test(d.has("AGENTS.md"), "AGENTS.md present", func() { d.bad("AGENTS.md missing") })
		d.test(d.has(".agents/skills/spec-driven-dev/SKILL.md"), "skills installed in .agents/skills", func() { d.bad(".agents/skills/spec-driven-dev missing (run sdkit init --host <host>)") })
		d.test(gitx.Config(root, "core.hooksPath") == ".githooks", "git hooks active (core.hooksPath=.githooks)", func() { d.warn("git hooks not active in this clone: git config core.hooksPath .githooks") })
		has := func(h string) bool {
			for _, x := range hs {
				if x == h {
					return true
				}
			}
			return false
		}
		files := func(host, pat, label string) {
			d.test(d.glob(pat), host+": "+label+" present", func() { d.bad("%s: %s missing", host, pat) })
		}
		valid := func(host, rel string) {
			if !d.has(rel) {
				d.bad("%s: %s missing", host, rel)
			} else if doctorValidJSON(d.s.abs(rel)) {
				d.ok("%s: %s valid", host, filepath.Base(rel))
			} else {
				d.bad("%s: %s is not valid JSON", host, rel)
			}
		}
		if has("codex") {
			d.test(d.has(".codex/config.toml"), "codex: .codex/config.toml present", func() { d.bad("codex: .codex/config.toml missing") })
			files("codex", ".codex/agents/*.toml", "role files")
		}
		if has("opencode") {
			files("opencode", ".opencode/agents/*.md", "role files")
			d.test(d.has(".opencode/plugins/spec-driven-kit-guard.js"), "opencode: guard plugin present", func() { d.bad("opencode: .opencode/plugins/spec-driven-kit-guard.js missing") })
		}
		if has("cursor") {
			files("cursor", ".cursor/agents/*.md", "role files")
			valid("cursor", ".cursor/hooks.json")
		}
		if has("gemini") {
			files("gemini", ".gemini/agents/*.md", "role files")
			valid("gemini", ".gemini/settings.json")
		}
	}
	return hs
}

// versions: kit version of the project, drift of managed copies and of the shared files; plugin vs binary.
func (d *doctor) versions(dir string) {
	s := d.s
	pv, err := os.ReadFile(s.abs(dir + "/.scaffold-version"))
	switch v := strings.TrimSpace(string(pv)); {
	case err != nil || v == "":
		d.warn("no %s/.scaffold-version (set up before versions were recorded): run sdkit upgrade", dir)
	case v == s.Version:
		d.ok("project is on kit %s", s.Version)
	default:
		d.warn("project was set up with kit %s, this binary is %s: run sdkit upgrade", v, s.Version)
	}
	if b, err := os.ReadFile(s.abs(dir + "/.scaffold-manifest")); err == nil {
		n := 0
		for _, l := range splitLines(string(b)) {
			f := strings.Split(l, "\t")
			if len(f) < 2 {
				continue
			}
			want, err := s.kitFile(f[1])
			got, err2 := os.ReadFile(s.abs(f[0]))
			if err != nil || err2 != nil || s.renderManaged(want, f[0]) != string(got) {
				n++
			}
		}
		d.test(n == 0, "managed copies match the kit", func() { d.warn("%d managed file(s) differ from the kit: sdkit sync --check (then sdkit sync)", n) })
	}
	if d.has(dir + "/.scaffold-shared") {
		var out bytes.Buffer
		saved := s.Out
		s.Out = &out
		err := s.RefreshShared(true)
		s.Out = saved
		d.test(err == nil, "shared files carry the current kit block/entries", func() {
			d.warn("kit part of a shared file is out of date: %d file(s). Run sdkit sync (it rewrites only the kit's block/entries)", strings.Count(out.String(), "drift"))
		})
	} else {
		d.warn("no %s/.scaffold-shared: shared files (AGENTS.md, settings.json ...) predate block management; run sdkit upgrade", dir)
	}
	if root := d.getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		var p struct{ Version string }
		if b, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil && json.Unmarshal(b, &p) == nil && p.Version != "" && s.Version != "" {
			d.test(p.Version == s.Version, "plugin and sdkit binary are both "+s.Version,
				func() {
					d.warn("the plugin is %s but the sdkit binary is %s; update the older one (see docs/upgrading.md)", p.Version, s.Version)
				})
		}
	}
}

func (d *doctor) ciGate() {
	files := []string{"bitbucket-pipelines.yml", ".gitlab-ci.yml"}
	if m, _ := filepath.Glob(d.s.abs(".github/workflows/*.yml")); len(m) > 0 {
		for _, f := range m {
			files = append(files, ".github/workflows/"+filepath.Base(f))
		}
	}
	re := regexp.MustCompile(`ci-check`)
	for _, f := range files {
		if b, err := os.ReadFile(d.s.abs(f)); err == nil && re.Match(b) {
			d.ok("CI runs the spec-driven gate (ci-check)")
			return
		}
	}
	d.warn("no server-side gate in CI: local hooks can be skipped with --no-verify (sdkit init --ci bitbucket|github|gitlab)")
}

func (d *doctor) cliNotes(hosts []string) {
	cli := map[string]string{"claude": "claude", "codex": "codex", "opencode": "opencode", "cursor": "cursor-agent", "gemini": "gemini"}
	for _, h := range hosts {
		if doctorTool("", cli[h]) {
			d.ok("%s CLI installed", cli[h])
		} else {
			d.warn("%s CLI not found (only needed for sdkit run-role --host %s)", cli[h], h)
		}
	}
}

func (d *doctor) tools(env *envfile.Settings, envp string) {
	name := orDefault(env.String("STACK"), DefaultStack)
	if st := stack.Get(name); st != nil && envp != "analyst" {
		for _, t := range st.Tools() {
			switch {
			case doctorTool(d.s.Root, t.Name):
				d.ok("%s installed", t.Name)
			case t.Required:
				d.bad("%s missing: %s", t.Name, t.Hint)
			default:
				d.warn("%s missing: %s", t.Name, t.Hint)
			}
		}
	}
	if name == "react-native" {
		for _, sk := range []string{"building-native-ui", "expo-tailwind-setup"} {
			for _, dd := range []string{".agents/skills", ".claude/skills"} {
				if d.has(dd + "/" + sk) {
					d.warn("%s/%s is an outdated Expo skill (removed upstream): remove it and install the current set (claude plugin install expo@claude-plugins-official, or npx skills add expo/skills)", dd, sk)
					break
				}
			}
		}
	}
}

func (d *doctor) openspec(hosts []string) {
	if doctorTool("", "openspec") {
		d.ok("openspec CLI installed")
	} else {
		d.bad("openspec CLI missing: npm install -g @fission-ai/openspec (or: sdkit init --install-openspec)")
	}
	if !d.isDir("openspec") {
		return
	}
	if d.has(".claude/settings.json") || d.has(".claude/settings.local.json") || d.isDir(".claude/skills") {
		d.test(d.isDir(".claude/skills/openspec-propose"), "openspec skills present for Claude Code", func() {
			d.warn("openspec skills missing in .claude/skills: run 'openspec update' (or init with --tools claude)")
		})
	}
	if d.isDir(".agents/skills/spec-driven-dev") || d.has("AGENTS.md") {
		d.test(d.isDir(".agents/skills/openspec-propose"), "openspec skills present in .agents/skills", func() { d.warn("openspec skills missing in .agents/skills: run 'openspec update'") })
	}
}

// checklist lists what Jira and the pull-request host still need, most important first.
func (d *doctor) checklist(env *envfile.Settings, dir string) {
	s := d.s
	s.say("")
	s.say("setup checklist (Jira and pull requests)")
	n := 0
	todo := func(a, b string) {
		n++
		d.wrn++
		s.say("  TODO %d. %s", n, a)
		s.say("          %s", b)
	}
	loc := dir + "/sdkit.local.env"
	if !d.has(loc) {
		loc = dir + "/" + s.KitEnv
	}
	get := func(k string) string {
		if v := d.getenv(k); v != "" {
			return v
		}
		return env.String(k)
	}
	if env.String("TRACKER") != "jira" {
		todo("TRACKER is not jira", dir+"/"+s.KitEnv+": TRACKER=jira")
	}
	url := get("JIRA_BASE_URL")
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		todo("JIRA_BASE_URL (your Jira site)", dir+"/"+s.KitEnv+": JIRA_BASE_URL=https://your-company.atlassian.net")
	}
	if get("JIRA_PROJECT") == "" {
		todo("JIRA_PROJECT (the project key, e.g. TFF)", dir+"/"+s.KitEnv+": JIRA_PROJECT=TFF")
	}
	if get("JIRA_USER") == "" {
		todo("JIRA_USER (your Atlassian account e-mail)", loc+": JIRA_USER=you@company.com")
	}
	jtv := orDefault(get("JIRA_TOKEN_ENV"), "JIRA_API_TOKEN")
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(jtv) {
		jtv = ""
		todo("JIRA_TOKEN_ENV holds something that is not a variable name (a token pasted there is exposed: revoke it)", loc+": remove JIRA_TOKEN_ENV, set JIRA_API_TOKEN=<new token>")
	} else if get(jtv) == "" {
		todo(jtv+" (your Jira API token; create it at id.atlassian.com > Security > API tokens)", loc+": "+jtv+"=<token>   (git-ignored file) or export "+jtv+" in your shell")
	}
	if get("JIRA_EGRESS") != "allow" {
		todo("JIRA_EGRESS is not allow: sdkit jira will not call Jira", loc+": JIRA_EGRESS=allow")
	}
	d.prHost(get, todo, loc)
	if n == 0 && jtv != "" {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", strings.TrimSuffix(url, "/")+"/rest/api/2/myself", nil)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(get("JIRA_USER")+":"+get(jtv))))
		code := 0
		if resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req); err == nil {
			code = resp.StatusCode
			resp.Body.Close()
		}
		switch code {
		case 200:
			d.ok("Jira reachable and the credentials work")
		case 401, 403:
			todo(fmt.Sprintf("Jira refused the credentials (HTTP %d)", code), fmt.Sprintf("check JIRA_USER (account e-mail) and that %s is a valid, unexpired API token", jtv))
		case 0:
			d.warn("Jira not reachable from here (offline or blocked): credentials not verified")
		default:
			d.warn("Jira answered HTTP %d on the credential check", code)
		}
	}
	if n == 0 {
		d.ok("Jira and PR-host settings are complete")
	} else {
		s.say("  (%d item(s) above, in priority order; run 'sdkit doctor' again after each change)", n)
	}
}

// prHost reports the CLI the configured pull-request host needs (gh for github, glab for gitlab).
func (d *doctor) prHost(get func(string) string, todo func(a, b string), loc string) {
	host := get("PR_HOST")
	if host == "" || host == "auto" {
		o, err := gitx.Run(d.s.Root, "remote", "get-url", "origin")
		if err != nil {
			return
		}
		switch {
		case strings.Contains(o, "github.com"):
			host = "github"
		case strings.Contains(o, "gitlab."):
			host = "gitlab"
		default:
			return
		}
	}
	cli := map[string]string{"github": "gh", "gitlab": "glab"}[host]
	if cli != "" && !doctorTool("", cli) {
		todo("PR host: install the "+cli+" CLI", "(see docs/hosts.md; secrets go in "+loc+", never in the shared file)")
	}
}
