package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/stack"
)

// InitOptions are the flags of `sdkit init`.
type InitOptions struct {
	Hosts           []string // claude (default), codex, opencode, cursor, gemini
	Mode            string   // plugin (default) or copy
	Stack           string   // "" = detect
	CI              string   // "", bitbucket, github, gitlab
	Env             string   // "", standalone, analyst, developer
	MemoryVCS       string   // "", commit, ignore
	NoMemory        bool
	InstallOpenSpec bool

	skipOpenSpec bool // tests: do not depend on the openspec CLI of the machine
}

// Hook wiring written by init, per host (all call `sdkit hook`, no .sh): Claude Code `sdkit hook <event>` (shell
// string, so the kit's entries stay recognisable on a re-run), Cursor/Gemini CLI `sdkit hook --host <h> <event>`,
// OpenCode a JS plugin that spawns `sdkit`. Codex has no hook wiring (1.x had none either).
// Codex, Gemini CLI and OpenCode Windows wiring is unverified until the Windows CI run (STATUS.md Q3).

var stackOrder = []string{"go", "spring-boot", "react-native", "jvm", "nextjs", "python"}

// detectStack is the first stack (in the bash kit's order) that recognises root; "" when none does.
func detectStack(root string) string {
	for _, n := range stackOrder {
		if st := stack.Get(n); st != nil && st.Detect(root) {
			return n
		}
	}
	return ""
}

var (
	credsInURL   = regexp.MustCompile(`(https?://)[^@/]+@`)
	prespecPlain = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	openspecKeep = regexp.MustCompile(`Created|skills and|Config:|error|Error`)
)

func envVal(file, key string) string {
	b, _ := os.ReadFile(file)
	v := ""
	for _, l := range splitLines(string(b)) {
		if x, ok := strings.CutPrefix(l, key+"="); ok {
			v = x
		}
	}
	return v
}

func hasEnvKey(file, key string) bool {
	b, _ := os.ReadFile(file)
	for _, l := range splitLines(string(b)) {
		if strings.HasPrefix(l, key+"=") {
			return true
		}
	}
	return false
}

// setEnvLine rewrites every KEY=... line of file to KEY=value.
func setEnvLine(file, key, value string) {
	b, err := os.ReadFile(file)
	if err != nil {
		return
	}
	ls := splitLines(string(b))
	for i, l := range ls {
		if strings.HasPrefix(l, key+"=") {
			ls[i] = key + "=" + value
		}
	}
	_ = os.WriteFile(file, []byte(strings.Join(ls, "\n")+"\n"), 0o644)
}

func appendText(file, text string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(text)
		_ = f.Close()
	}
}

// envKey appends a persona setting when the file lacks it; existing lines are never rewritten.
func (s *Setup) envKey(file, persona, key, value, comment string) {
	if hasEnvKey(file, key) {
		return
	}
	appendText(file, fmt.Sprintf("\n# %s\n%s=%s\n", comment, key, value))
	s.say("  added  %s/%s: %s (%s)", s.KitDir, s.KitEnv, key, persona)
}

// installTree copies a kit directory tree (every file below src) as managed files below dest.
func (s *Setup) installTree(src, dest string) error {
	return fs.WalkDir(s.FS, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return s.InstallManaged(p, path.Join(dest, strings.TrimPrefix(p, src+"/")))
	})
}

func (s *Setup) installSkills(dest string) error {
	ents, err := fs.ReadDir(s.FS, "skills")
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() {
			if err := s.installTree("skills/"+e.Name(), dest+"/"+e.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// installGlob installs every kit file matching pattern into destDir under its own name.
func (s *Setup) installGlob(pattern, destDir string) error {
	m, _ := fs.Glob(s.FS, pattern)
	for _, f := range m {
		if err := s.InstallManaged(f, destDir+"/"+path.Base(f)); err != nil {
			return err
		}
	}
	return nil
}

// Init installs the kit's project side into s.Root (what bin/init.sh did). It returns the exit code.
func (s *Setup) Init(o InitOptions) int {
	code, err := s.init(o)
	if err != nil {
		s.say("sdkit init: %v", err)
		return 1
	}
	return code
}

func (s *Setup) init(o InitOptions) (int, error) {
	hosts := o.Hosts
	if len(hosts) == 0 {
		hosts = []string{"claude"}
	}
	want := func(h string) bool {
		for _, x := range hosts {
			if x == h {
				return true
			}
		}
		return false
	}
	mode := o.Mode
	if mode == "" {
		mode = "plugin"
	}
	root := s.Root
	if _, err := gitx.Run(root, "rev-parse", "--git-dir"); err != nil {
		return 1, fmt.Errorf("%s is not a git repository", root)
	}
	if k := ResolveKit(root); k != "" {
		s.KitDir = k
	} else if upgradeLegacy(root) {
		return 1, fmt.Errorf("pre-1.0 layout found: run the last 1.x release's `upgrade --migrate --apply` first")
	}
	s.KitEnv = "sdkit.env"
	envFile := filepath.Join(root, s.KitDir, s.KitEnv)
	cur := strings.Trim(envVal(envFile, "SDKIT_ENV"), "\"' ")
	switch cur {
	case "", "standalone", "analyst", "developer":
	default:
		return 2, fmt.Errorf("SDKIT_ENV='%s' in %s/%s is not standalone, analyst or developer", cur, s.KitDir, s.KitEnv)
	}
	if cur != "" && o.Env != "" && cur != o.Env {
		return 2, fmt.Errorf("%s/%s already has SDKIT_ENV=%s; init does not rewrite it. Edit the line by hand to switch to %s", s.KitDir, s.KitEnv, cur, o.Env)
	}
	persona := cur
	if persona == "" {
		persona = o.Env
	}
	if persona == "" {
		persona = "standalone"
	}

	st := o.Stack
	if st == "" && persona == "analyst" {
		st = "generic" // a project-specs repo has no stack
	}
	if st == "" {
		st = detectStack(root)
	}
	if st == "" {
		st = "generic"
		s.say("no known stack detected: using 'generic'. Set VERIFY_FAST_CMD / VERIFY_TEST_CMD in sdkit.env, or pass --stack (one of: %s)", strings.Join(stack.Names(), ", "))
	}
	if _, err := fs.Stat(s.FS, "stacks/"+st); err != nil {
		return 1, fmt.Errorf("unsupported stack: %s", st)
	}
	s.Stack = st

	// the plugin is installed from the repository declared in plugin.json, with any credentials stripped
	if s.MarketplaceURL == "" {
		if b, err := fs.ReadFile(s.FS, ".claude-plugin/plugin.json"); err == nil {
			var p struct{ Repository string }
			_ = json.Unmarshal(b, &p)
			s.MarketplaceURL = credsInURL.ReplaceAllString(p.Repository, "$1")
		}
	}
	base := ""
	if b, err := gitx.Run(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		base = strings.TrimPrefix(b, "origin/")
	}
	for _, c := range []string{"develop", "main", "master"} {
		if base == "" && gitx.RevParse(root, c) != "" {
			base = c
		}
	}
	if base == "" {
		base = "main"
	}
	s.BaseBranch, s.ProjectName = base, filepath.Base(root)

	s.say("spec-driven-kit %s -> %s (host=%s dir=%s mode=%s stack=%s base=%s%s)", s.Version, root, strings.Join(hosts, ","), s.KitDir, mode, st, base,
		map[bool]string{true: " env=" + o.Env}[o.Env != ""])
	if err := os.MkdirAll(filepath.Join(root, s.KitDir), 0o755); err != nil {
		return 1, err
	}
	if err := os.WriteFile(s.manifestNew(), nil, 0o644); err != nil {
		return 1, err
	}
	steps := []func() error{
		func() error { return s.initProject(o, persona, st) },
		func() error { return s.initPersona(o, persona) },
		func() error { return s.initMemory(o) },
	}
	for _, f := range steps {
		if err := f(); err != nil {
			return 1, err
		}
	}
	if o.CI != "" {
		if err := s.InstallCI(o.CI); err != nil {
			return 1, err
		}
	}
	if err := s.initHosts(hosts, mode, want); err != nil {
		return 1, err
	}
	pending := !o.skipOpenSpec && persona == "analyst" && s.initOpenSpec(hosts, o.InstallOpenSpec) // only generate-specs outputs an OpenSpec change
	if persona != "standalone" {
		s.say("Next: run 'sdkit doctor'. Its setup checklist lists the Jira and pull-request values still to fill in, most important first.")
	}
	if err := s.FinishManifest(); err != nil {
		return 1, err
	}
	s.hideGenerated()
	s.initFarewell(hosts, want, st, pending)
	return 0, nil
}

// initProject writes the project-owned files of <project-dir>: rules, profile, settings, gitignore.
func (s *Setup) initProject(o InitOptions, persona, st string) error {
	say, dir := s.say, s.KitDir
	envFile := filepath.Join(s.Root, dir, s.KitEnv)
	say("rules and stack defaults (project-owned: edit freely)")
	rules, _ := fs.Glob(s.FS, "core/rules/*.md")
	more, _ := fs.Glob(s.FS, "stacks/"+st+"/rules/*.md")
	for _, f := range append(rules, more...) {
		if err := s.InstallTemplate(f, dir+"/rules/"+path.Base(f)); err != nil {
			return err
		}
	}
	if _, err := fs.Stat(s.FS, "stacks/"+st+"/golangci.yml"); err == nil {
		if err := s.InstallTemplate("stacks/"+st+"/golangci.yml", dir+"/golangci.yml"); err != nil {
			return err
		}
	}
	if ents, _ := fs.ReadDir(s.FS, "stacks/"+st+"/verify.d.example"); persona != "analyst" { // no verify in a project-specs repo
		for _, e := range ents {
			if err := s.InstallTemplate("stacks/"+st+"/verify.d.example/"+e.Name(), dir+"/verify.d/"+e.Name()); err != nil {
				return err
			}
		}
	}

	say("project profile (project-owned: fill in)")
	_, statErr := os.Stat(envFile)
	envNew := statErr != nil
	if !envNew && o.Env != "" && !hasEnvKey(envFile, "SDKIT_ENV") {
		appendText(envFile, fmt.Sprintf("\n# Install persona: standalone | analyst | developer (set by sdkit init --env)\nSDKIT_ENV=%s\n", persona))
	}
	pit := "templates/known-pitfalls.md"
	if _, err := fs.Stat(s.FS, "stacks/"+st+"/known-pitfalls.md"); err == nil {
		pit = "stacks/" + st + "/known-pitfalls.md"
	}
	for _, f := range [][2]string{{"templates/sdkit.env", dir + "/" + s.KitEnv}, {"templates/project.md", dir + "/project.md"}, {pit, dir + "/known-pitfalls.md"},
		{"templates/pricing.json", dir + "/pricing.json"}, {"templates/adr/TEMPLATE.md", dir + "/adr/TEMPLATE.md"}, {"templates/sdkit.gitignore", dir + "/.gitignore"}} {
		if err := s.InstallTemplate(f[0], f[1]); err != nil {
			return err
		}
		if f[0] == "templates/sdkit.env" && envNew && persona != "standalone" {
			setEnvLine(envFile, "SDKIT_ENV", persona)
		}
	}
	// 1.0.0 kept every setting in one file: the personal ones move to sdkit.local.env (git-ignored), the shared file is committed
	abs := filepath.Join(s.Root, dir)
	if err := s.SplitPersonalEnv(abs, s.KitEnv); err != nil {
		return err
	}
	if err := s.InstallTemplate("templates/sdkit.local.env", dir+"/sdkit.local.env"); err != nil {
		return err
	}
	gi := filepath.Join(abs, ".gitignore")
	if b, err := os.ReadFile(gi); err == nil && !strings.Contains("\n"+string(b), "\nsdkit.local.env\n") {
		var keep []string
		for _, l := range splitLines(string(b)) {
			if l != "sdkit.env" && l != "sdkit.env.bak" {
				keep = append(keep, l)
			}
		}
		_ = os.WriteFile(gi, []byte(strings.Join(keep, "\n")+"\n\n# personal settings (sdkit.env is shared and committed)\nsdkit.local.env\nsdkit.local.env.bak\n# a root .gitignore rule such as *.env would hide the shared file; it holds settings only, never secrets\n!sdkit.env\n"), 0o644)
		say("  update %s/.gitignore: sdkit.local.env is personal now, sdkit.env is shared", dir)
	}
	// settings added to the templates since these files were written arrive with their defaults; existing lines are never touched
	if !envNew {
		for _, f := range [][2]string{{s.KitEnv, "templates/sdkit.env"}, {"sdkit.local.env", "templates/sdkit.local.env"}} {
			added, err := s.AppendMissingEnv(filepath.Join(abs, f[0]), f[1])
			if err != nil {
				return err
			}
			if len(added) > 0 {
				say("  added  %s/%s: %s (defaults; yours are untouched)", dir, f[0], strings.Join(added, " "))
			}
		}
	} else if o.NoMemory {
		if envVal(envFile, "MEMORY") == "1" {
			setEnvLine(envFile, "MEMORY", "")
		}
	}
	return nil
}

// initPersona adds the settings and files only the analyst and developer installs carry.
func (s *Setup) initPersona(o InitOptions, persona string) error {
	say, dir := s.say, s.KitDir
	envFile := filepath.Join(s.Root, dir, s.KitEnv)
	switch persona {
	case "developer":
		s.envKey(envFile, persona, "SDD_MANAGED", "on", "Managed execution (SDKIT_ENV=developer): tasks come from Jira, approved specs from the read-only submodule below.")
		s.envKey(envFile, persona, "SPECS_DIR", "specs", "Directory of the approved-specs git submodule (read-only for developers).")
		s.envKey(envFile, persona, "TRACKER", "jira", "Issue tracker for managed execution (see the Jira keys further down).")
		s.envKey(envFile, persona, "SPEC_REPO", "", "Hint: git URL of the project-specs repo to add as the submodule: git submodule add <SPEC_REPO> specs")
	case "analyst":
		s.envKey(envFile, persona, "PRESPEC_DIR", "prespec", "Git-ignored directory for pre-specs (raw input). Its content is never committed. Default: prespec")
		s.envKey(envFile, persona, "SPEC_REVIEW_MAX_ROUNDS", "4", "Most review rounds per spec before a human decides. Default: 4")
		s.envKey(envFile, persona, "ANALYST_LANGUAGE", "tr", "Language of the analyst's prose and of the generated specs (ISO 639-1 code or a name, e.g. tr, en). Default: tr")
	}
	if persona == "standalone" {
		return nil
	}
	// Both personas work against Jira and the PR host. Only exact old defaults change (TRACKER=none); values you set are never touched.
	if t := envVal(envFile, "TRACKER"); t == "" || t == "none" {
		if hasEnvKey(envFile, "TRACKER") {
			setEnvLine(envFile, "TRACKER", "jira")
			say("  set    %s/%s: TRACKER=jira (%s works against Jira)", dir, s.KitEnv, persona)
		}
	}
	s.envKey(envFile, persona, "JIRA_BASE_URL", "", "Your Jira site, e.g. https://your-company.atlassian.net  (FILL IN: sdkit doctor lists what is missing)")
	s.envKey(envFile, persona, "JIRA_PROJECT", "", "Jira project key the tasks live in, e.g. TFF  (FILL IN)")
	s.envKey(envFile, persona, "JIRA_SUBTASK_TYPE", "Sub-task", "Issue type of the subtasks the kit creates.")
	s.envKey(envFile, persona, "JIRA_TASK_TYPE", "Task", "Issue type of the group task a spec task becomes when it has role subtasks (it sits under the parent, its role subtasks under it).")
	s.envKey(envFile, persona, "JIRA_API_VERSION", "3", "Jira REST version. 3 = Jira Cloud (descriptions are sent as Atlassian Document Format); 2 = Data Center / plain text.")
	loc := filepath.Join(s.Root, dir, "sdkit.local.env")
	if _, err := os.Stat(loc); err == nil && !hasEnvKey(loc, "JIRA_EGRESS") {
		appendText(loc, fmt.Sprintf("\n# Jira egress: allow = sdkit may call the Jira API from this machine (set by sdkit init for the %s env; remove to turn the network off).\nJIRA_EGRESS=allow\n", persona))
		say("  added  %s/sdkit.local.env: JIRA_EGRESS=allow", dir)
	}
	// review rubrics per artifact (project-owned: edit freely; existing files are kept)
	rub, _ := fs.Glob(s.FS, "templates/review.d/*")
	for _, f := range rub {
		if err := s.InstallTemplate(f, dir+"/review.d/"+path.Base(f)); err != nil {
			return err
		}
	}
	if persona != "analyst" {
		return nil
	}
	say("pre-spec directory (git-ignored; nothing in it is committed)")
	ps := strings.Trim(envVal(envFile, "PRESPEC_DIR"), "\"' ")
	if ps == "" {
		ps = "prespec"
	}
	if strings.HasPrefix(ps, "/") || strings.Contains(ps, "..") || !prespecPlain.MatchString(ps) {
		return fmt.Errorf("PRESPEC_DIR '%s' must be a plain relative path inside the repo", ps)
	}
	ps = strings.TrimSuffix(ps, "/")
	if _, err := os.Stat(filepath.Join(s.Root, ps, "README.md")); err != nil {
		b, err := s.kitFile("templates/prespec/README.md")
		if err != nil {
			return err
		}
		if err := put(filepath.Join(s.Root, ps, "README.md"), []byte(b), 0o644); err != nil {
			return err
		}
		say("  wrote  %s/README.md", ps)
	}
	rootGI := filepath.Join(s.Root, ".gitignore")
	if b, _ := os.ReadFile(rootGI); !strings.Contains("\n"+string(b), "\n/"+ps+"/\n") {
		appendText(rootGI, fmt.Sprintf("\n# pre-specs are working input, never committed (sdkit analyst env)\n/%s/\n", ps))
		say("  wrote  .gitignore: /%s/", ps)
	}
	return nil
}

// initMemory creates the project memory index. The entries are the developer's.
func (s *Setup) initMemory(o InitOptions) error {
	pe := filepath.Join(s.Root, s.KitDir, s.KitEnv)
	if envVal(pe, "MEMORY") != "1" {
		return nil
	}
	s.say("project memory (shared through git; entries are yours)")
	if o.MemoryVCS != "" {
		setEnvLine(pe, "MEMORY_VCS", o.MemoryVCS)
	}
	dir := envVal(pe, "MEMORY_DIR")
	if dir == "" {
		dir = "memory"
	}
	if filepath.IsAbs(dir) || strings.HasPrefix(dir, "/") {
		s.say("  MEMORY_DIR is outside the repo; no .gitignore change")
	} else {
		gi := filepath.Join(s.Root, s.KitDir, ".gitignore")
		b, _ := os.ReadFile(gi)
		if envVal(pe, "MEMORY_VCS") == "ignore" && !strings.Contains("\n"+string(b), "\n"+dir+"/\n") {
			appendText(gi, fmt.Sprintf("\n# project memory kept personal (MEMORY_VCS=ignore)\n%s/\n", dir))
			s.say("  wrote  %s/.gitignore: %s/", s.KitDir, dir)
		}
		dir = filepath.Join(s.Root, s.KitDir, dir)
	}
	index := filepath.Join(dir, "INDEX.md")
	if _, err := os.Stat(index); err != nil {
		text := "# Knowledge index\n<!-- managed by sdkit memory: one line per entry; read an entry only when its line looks relevant -->\n## Linked\n"
		if _, err := os.Stat(filepath.Join(s.Root, s.KitDir, "known-pitfalls.md")); err == nil {
			text += "- " + s.KitDir + "/known-pitfalls.md: past bug classes\n"
		}
		if st, err := os.Stat(filepath.Join(s.Root, s.KitDir, "adr")); err == nil && st.IsDir() {
			text += "- " + s.KitDir + "/adr/: architecture decisions\n"
		}
		if err := put(index, []byte(text), 0o644); err != nil {
			return err
		}
	}
	s.say("  wrote  %s/%s/INDEX.md", s.KitDir, filepath.Base(dir))
	return nil
}

// initHosts installs the per-host files. Nothing is copied but skills, roles and the host's own config: the
// guards are `sdkit hook`, there are no scripts any more.
func (s *Setup) initHosts(hosts []string, mode string, want func(string) bool) error {
	other := false
	for _, h := range hosts {
		other = other || h != "claude"
	}
	if mode == "copy" && want("claude") {
		s.say("kit files copied into .claude/ (managed: sync updates them)")
		if err := s.installSkills(".claude/skills"); err != nil {
			return err
		}
		if err := s.installGlob("agents/*.md", ".claude/agents"); err != nil {
			return err
		}
	}
	if other {
		s.say("skills, AGENTS.md and git hooks for the non-Claude hosts")
		if err := s.installSkills(".agents/skills"); err != nil {
			return err
		}
		if err := s.InstallShared("block-md", "AGENTS.md", "templates/blocks/AGENTS.kit.md"); err != nil {
			return err
		}
		if err := s.installGlob("adapters/git/*", ".githooks"); err != nil {
			return err
		}
	}
	if want("codex") {
		s.say("codex: roles")
		if err := s.installGlob("adapters/codex/agents/*.toml", ".codex/agents"); err != nil {
			return err
		}
		if err := s.InstallShared("block-hash", ".codex/config.toml", "adapters/codex/config.toml.snippet"); err != nil {
			return err
		}
	}
	if want("opencode") {
		s.say("opencode: roles and guard plugin")
		if err := s.installGlob("adapters/opencode/agents/*.md", ".opencode/agents"); err != nil {
			return err
		}
		if err := s.InstallManaged("adapters/opencode/plugins/spec-driven-kit-guard.js", ".opencode/plugins/spec-driven-kit-guard.js"); err != nil {
			return err
		}
	}
	if want("cursor") {
		s.say("cursor: roles and hooks")
		if err := s.installGlob("adapters/cursor/agents/*.md", ".cursor/agents"); err != nil {
			return err
		}
		if err := s.InstallShared("hooks-flat", ".cursor/hooks.json", "adapters/cursor/hooks.json"); err != nil {
			return err
		}
	}
	if want("gemini") {
		s.say("gemini: roles, context file and hooks")
		if err := s.installGlob("adapters/gemini/agents/*.md", ".gemini/agents"); err != nil {
			return err
		}
		if err := s.InstallShared("hooks-grouped", ".gemini/settings.json", "adapters/gemini/settings.json"); err != nil {
			return err
		}
	}
	if want("claude") {
		s.say("settings")
		if err := s.InstallShared("claude-settings", ".claude/settings.local.json", "templates/settings."+mode+".json"); err != nil {
			return err
		}
		return s.InstallShared("block-md", ".claude/CLAUDE.md", "templates/blocks/CLAUDE.kit.md")
	}
	return nil
}

// initOpenSpec runs `openspec init` for the hosts (the CLI comes from npm; the kit does not vendor it). It reports
// whether OpenSpec is still missing.
func (s *Setup) initOpenSpec(hosts []string, install bool) (pending bool) {
	ctx, list := context.Background(), strings.Join(hosts, ",")
	s.say("openspec (required by the workflow)")
	if _, err := proc.LookPath("openspec"); err != nil && install {
		if _, err := proc.LookPath("npm"); err != nil {
			s.say("  FAIL   npm not found: install Node.js first (brew install node), then re-run with --install-openspec")
		} else {
			s.say("  installing @fission-ai/openspec (npm install -g)")
			if r, err := proc.RunShell(ctx, s.Root, nil, "npm install -g @fission-ai/openspec"); err != nil || r.Code != 0 {
				s.say("  FAIL   npm install -g @fission-ai/openspec did not succeed; install it yourself")
			}
		}
	}
	if _, err := proc.LookPath("openspec"); err != nil {
		s.say("  MISSING openspec CLI. Install it, then run this again (or: openspec init --tools %s):", list)
		s.say("           npm install -g @fission-ai/openspec        (or re-run with --install-openspec)")
		return true
	}
	if st, err := os.Stat(filepath.Join(s.Root, "openspec")); err == nil && st.IsDir() {
		s.say("  kept   openspec/ (exists; run 'openspec update' in the repo if its skills are missing for a host)")
		return false
	}
	r, _ := proc.RunShell(ctx, s.Root, nil, "openspec init --tools "+list+" --no-animation")
	for _, l := range splitLines(r.Stdout + r.Stderr) {
		if openspecKeep.MatchString(l) {
			s.say("  %s", l)
		}
	}
	if st, err := os.Stat(filepath.Join(s.Root, "openspec")); err == nil && st.IsDir() {
		s.say("  wrote  openspec/ and OpenSpec skills for: %s", list)
		return false
	}
	s.say("  FAIL   openspec init did not create openspec/; run it by hand: openspec init --tools %s", list)
	return true
}

// hideGenerated hides the generated files outside <project-dir> from git for this clone (.git/info/exclude).
// AGENTS.md and CLAUDE.md carry the kit block and are meant to be committed.
func (s *Setup) hideGenerated() {
	var gen []string
	add := func(dest string) {
		if dest != "" && !strings.HasPrefix(dest, s.KitDir+"/") {
			gen = append(gen, dest)
		}
	}
	mb, _ := os.ReadFile(filepath.Join(s.Root, s.KitDir, ".scaffold-manifest"))
	for _, l := range splitLines(string(mb)) {
		add(strings.Split(l, "\t")[0])
	}
	sb, _ := os.ReadFile(filepath.Join(s.Root, s.KitDir, SharedFileName))
	for _, l := range splitLines(string(sb)) {
		if f := strings.Split(l, "\t"); len(f) > 1 && f[0] != "block-md" {
			add(f[1])
		}
	}
	if len(gen) > 0 {
		_ = ExcludeUntracked(s.Root, gen...)
	}
}

func (s *Setup) initFarewell(hosts []string, want func(string) bool, st string, pending bool) {
	say, dir := s.say, s.KitDir
	say("")
	say("done. next: fill in %s/project.md and review %s/%s, rename the verify.d/*.example files you want, then run:", dir, dir, s.KitEnv)
	say("  sdkit doctor")
	say("Commit %s/ (its .gitignore keeps sdkit.local.env, work/ and generated files out), AGENTS.md and the kit block in .claude/CLAUDE.md. Each developer runs 'sdkit init' once on their machine.", dir)
	if len(hosts) > 1 || !want("claude") {
		say("Other hosts: AGENTS.md is the shared entry file; .agents/ .githooks/ and the host files are generated per machine. Each clone runs once:  git config core.hooksPath .githooks")
		if want("codex") {
			say("Codex: check that your Codex version has multi-agent enabled (see .codex/config.toml); start a task with $spec-driven-dev.")
		}
		if want("opencode") {
			say("OpenCode: roles in .opencode/agents (shell permission key is '%s'; on older releases set OPENCODE_SHELL_KEY=bash in %s/%s and run sdkit sync), guard plugin in .opencode/plugins.", envVal(filepath.Join(s.Root, dir, s.KitEnv), "OPENCODE_SHELL_KEY"), dir, s.KitEnv)
		}
		if want("cursor") {
			say("Cursor: the kit's hooks were merged into .cursor/hooks.json next to yours; hooks and subagents may be IDE-only (see docs/guide.md section 13).")
		}
		if want("gemini") {
			say("Gemini CLI: the kit's hooks and context file were merged into .gemini/settings.json next to your settings; subagents may need the experimental flag.")
		}
	}
	if st == "react-native" {
		say("React Native: optional third-party skills (not part of the kit; install them yourself):")
		if s.hasExpo() {
			say("  Expo (expo-router, expo-native-ui, expo-data-fetching ...): claude plugin install expo@claude-plugins-official   other hosts: npx skills add expo/skills")
		}
		say("  React Native rules: npx skills add vercel-labs/agent-skills   performance: npx skills add callstackincubator/agent-skills")
		say("  (npx skills add writes .agents/skills/; doctor does not count it as a host install)")
	}
	if pending {
		say("NOTE: the workflow needs OpenSpec; doctor will fail until it is installed and initialised.")
	}
}

// hasExpo is true when a package.json within three levels declares expo.
func (s *Setup) hasExpo() (found bool) {
	_ = filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(s.Root, p)
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || strings.Count(filepath.ToSlash(rel), "/") >= 3) {
			return filepath.SkipDir
		}
		if d.Name() == "package.json" {
			var j struct{ Dependencies, DevDependencies map[string]any }
			if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &j) == nil {
				_, a := j.Dependencies["expo"]
				_, b := j.DevDependencies["expo"]
				found = found || a || b
			}
		}
		return nil
	})
	return
}
