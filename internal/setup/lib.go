// Package setup is the project-side installer library: file installers, shared-file
// merges (marked blocks, JSON keys), settings upgrades and git exclude handling.
package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/store"
)

// DefaultStack is used when no stack is set (SDKIT_DEFAULT_STACK of the bash kit).
const DefaultStack = "go"

// Setup is one install run against a target repository.
type Setup struct {
	Root           string // target repository root (TARGET)
	KitDir         string // project directory relative to Root
	KitEnv         string // settings file name inside KitDir
	Version        string // kit version written into markers and the manifest
	ProjectName    string
	BaseBranch     string
	Stack          string
	MarketplaceURL string
	KitHooks       string // value of __KIT_HOOKS__ (default ./<KitDir>/hooks)
	Force          bool
	FS             fs.FS     // kit data (templates, adapters, stacks ...); the embedded copy by default
	Out            io.Writer // progress lines (say)
}

// New returns a Setup for root with the kit's embedded data and the project directory defaults.
func New(root string) *Setup {
	s := &Setup{Root: root, KitDir: ".sdkit", KitEnv: "sdkit.env", FS: kit.FS, Out: os.Stdout}
	if b, err := fs.ReadFile(kit.FS, ".claude-plugin/plugin.json"); err == nil {
		var v struct{ Version string }
		_ = json.Unmarshal(b, &v)
		s.Version = v.Version
	}
	return s
}

// ResolveKit returns the project directory of target ("" when it has no sdkit.env yet).
// Pre-1.0 layouts are not recognised any more (D2: upgrade refuses them).
func ResolveKit(target string) string {
	if p := project.Resolve(target); p.Found {
		return p.KitDir
	}
	return ""
}

func (s *Setup) say(format string, a ...any) { fmt.Fprintf(s.Out, format+"\n", a...) }

func (s *Setup) abs(rel string) string { return filepath.Join(s.Root, filepath.FromSlash(rel)) }

func (s *Setup) stack() string {
	if s.Stack != "" {
		return s.Stack
	}
	if p := project.Resolve(s.Root); p.Found {
		e := envfile.New()
		e.Quiet = true
		if e.LoadFile(filepath.Join(s.Root, p.KitDir, p.EnvFile)) == nil && e.String("STACK") != "" {
			return e.String("STACK")
		}
	}
	return DefaultStack
}

func (s *Setup) hooks() string {
	if s.KitHooks != "" {
		return s.KitHooks
	}
	return "./" + s.KitDir + "/hooks"
}

// render substitutes the placeholders shared files use.
func (s *Setup) render(text string) string {
	return strings.NewReplacer("__KIT_ENV__", s.KitEnv, "__KIT_DIR__", s.KitDir, "__KIT_HOOKS__", s.hooks()).Replace(text)
}

func (s *Setup) kitFile(rel string) (string, error) {
	b, err := fs.ReadFile(s.FS, rel)
	return string(b), err
}

// put writes data to path, creating parent directories.
func put(p string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, perm)
}

func shaFile(p string) string {
	b, _ := os.ReadFile(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Setup) manifestNew() string {
	return filepath.Join(s.Root, s.KitDir, ".scaffold-manifest.new")
}

// recordManaged remembers what was installed so sync can tell local edits from upstream changes.
func (s *Setup) recordManaged(dest, src string) error {
	return store.AppendLine(s.manifestNew(), dest+"\t"+src+"\t"+shaFile(s.abs(dest)))
}

// FinishManifest publishes the manifest and the version file after an install run.
func (s *Setup) FinishManifest() error {
	dir := filepath.Join(s.Root, s.KitDir)
	if err := os.Rename(s.manifestNew(), filepath.Join(dir, ".scaffold-manifest")); err != nil {
		return err
	}
	return put(filepath.Join(dir, ".scaffold-version"), []byte(s.Version+"\n"), 0o644)
}

var shellLine = regexp.MustCompile(`(?m)^  shell:$`)

// renderManaged is the bytes to install: the source, except where a project setting changes the
// kit's output (OPENCODE_SHELL_KEY: the permission key OpenCode roles use for the shell tool).
func (s *Setup) renderManaged(text, dest string) string {
	if !strings.HasPrefix(dest, ".opencode/agents/") || !strings.HasSuffix(dest, ".md") {
		return text
	}
	b, _ := os.ReadFile(filepath.Join(s.Root, s.KitDir, s.KitEnv))
	key := ""
	for _, l := range splitLines(string(b)) {
		if v, ok := strings.CutPrefix(l, "OPENCODE_SHELL_KEY="); ok {
			key = v
		}
	}
	if key != "" && key != "shell" {
		return shellLine.ReplaceAllLiteralString(text, "  "+key+":")
	}
	return text
}

// InstallManaged copies a kit-owned file; an existing different file is kept unless Force.
func (s *Setup) InstallManaged(src, dest string) error {
	text, err := s.kitFile(src)
	if err != nil {
		return err
	}
	text = s.renderManaged(text, dest)
	p := s.abs(dest)
	if old, err := os.ReadFile(p); err == nil && string(old) != text && !s.Force {
		s.say("  kept   %s (exists and differs; use --force to overwrite)", dest)
	} else {
		perm := os.FileMode(0o644)
		if strings.HasPrefix(dest, ".githooks/") || strings.HasSuffix(dest, ".sh") {
			perm = 0o755 // embedded files carry no mode; git hooks and scripts must run
		}
		if err := put(p, []byte(text), perm); err != nil {
			return err
		}
		_ = os.Chmod(p, perm)
		s.say("  wrote  %s", dest)
	}
	return s.recordManaged(dest, src)
}

// InstallTemplate creates a project-owned file once from a template; it is never overwritten, not even with Force.
func (s *Setup) InstallTemplate(src, dest string) error {
	text, err := s.kitFile(src)
	if err != nil {
		return err
	}
	p := s.abs(dest)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		s.say("  kept   %s (project-owned, exists)", dest)
		return nil
	}
	url := s.MarketplaceURL
	if url == "" {
		url = "https://example.invalid/spec-driven-kit.git"
	}
	for _, r := range [][2]string{{"__KIT_ENV__", s.KitEnv}, {"__BASE_BRANCH__", s.BaseBranch}, {"__PROJECT_NAME__", s.ProjectName},
		{"__KIT_DIR__", s.KitDir}, {"__SDKIT_VERSION__", s.Version}, {"__STACK__", s.stack()}, {"__KIT_HOOKS__", s.hooks()}, {"__MARKETPLACE_URL__", url}} {
		text = strings.ReplaceAll(text, r[0], r[1])
	}
	if err := put(p, []byte(text), 0o644); err != nil {
		return err
	}
	s.say("  wrote  %s (project-owned: edit it)", dest)
	return nil
}

// DetectHosts lists the hosts a repo was set up for (claude, codex, opencode, cursor, gemini); claude when none.
func DetectHosts(t string) []string {
	has := func(p string, dir bool) bool {
		st, err := os.Stat(filepath.Join(t, filepath.FromSlash(p)))
		return err == nil && st.IsDir() == dir
	}
	var h []string
	if has(".claude/settings.json", false) || has(".claude/settings.local.json", false) || has(".claude/CLAUDE.md", false) {
		h = append(h, "claude")
	}
	if has(".codex/config.toml", false) || has(".codex/agents", true) {
		h = append(h, "codex")
	}
	for _, x := range []string{"opencode", "cursor", "gemini"} {
		if has("."+x+"/agents", true) {
			h = append(h, x)
		}
	}
	// AGENTS.md or the kit skill in .agents/skills with no other host directory is a Codex setup (0.3 layout)
	if (len(h) == 0 || (len(h) == 1 && h[0] == "claude")) && (has("AGENTS.md", false) || has(".agents/skills/spec-driven-dev", true)) {
		h = append(h, "codex")
	}
	if len(h) == 0 {
		h = []string{"claude"}
	}
	return h
}

// DetectMode is copy when the kit's files were copied into the repo (2.0: .claude/agents/ or a skill; 1.x: .claude/hooks), else plugin.
func DetectMode(t string) string {
	for _, d := range []string{"hooks", "agents/verifier.md", "skills/spec-driven-dev"} {
		if _, err := os.Stat(filepath.Join(t, ".claude", filepath.FromSlash(d))); err == nil {
			return "copy"
		}
	}
	return "plugin"
}

var (
	envKey   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=`)
	colonKey = regexp.MustCompile(`^: "\$\{[A-Z_]+:=`)
	blankRe  = regexp.MustCompile(`^\s*$`)
)

// settingKey is the setting a line defines (KEY=... or : "${KEY:=...}"), else "".
func settingKey(l string) string {
	if m := envKey.FindString(l); m != "" {
		return m[:len(m)-1]
	}
	if m := colonKey.FindString(l); m != "" {
		return m[5 : len(m)-2]
	}
	return ""
}

// AppendMissingEnv adds the settings the template has and dest lacks, with their comments, after the
// existing content. Existing lines and values are never touched. It returns the added keys.
func (s *Setup) AppendMissingEnv(dest, tpl string) ([]string, error) {
	old, err := os.ReadFile(dest)
	if err != nil {
		return nil, err
	}
	text, err := s.kitFile(tpl)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, l := range splitLines(string(old)) {
		if k := settingKey(l); k != "" {
			have[k] = true
		}
	}
	var add strings.Builder
	var keys []string
	buf := ""
	for _, l := range splitLines(text) {
		if strings.HasPrefix(l, "#") || blankRe.MatchString(l) {
			buf += l + "\n"
			continue
		}
		if k := settingKey(l); k != "" && !have[k] {
			add.WriteString(buf + l + "\n")
			keys = append(keys, k)
		}
		buf = ""
	}
	if add.Len() == 0 {
		return nil, nil
	}
	out := string(old)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += fmt.Sprintf("\n# --- added by sdkit %s: settings new since this file was created (your values above are untouched) ---\n", s.Version) + add.String()
	return keys, os.WriteFile(dest, []byte(out), 0o644)
}

// SplitPersonalEnv moves the personal settings (the keys of templates/sdkit.local.env) of a one-file repo
// to sdkit.local.env with their values. It does nothing when the local file exists. Only comment lines the
// kit itself wrote above a moved setting are removed; the user's own lines stay.
func (s *Setup) SplitPersonalEnv(dir, envName string) error {
	f, loc := filepath.Join(dir, envName), filepath.Join(dir, "sdkit.local.env")
	old, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	if _, err := os.Stat(loc); err == nil {
		return nil
	}
	tpl, err := s.kitFile("templates/sdkit.local.env")
	if err != nil {
		return err
	}
	pk, kc := map[string]bool{}, map[string]bool{}
	for _, l := range splitLines(tpl) {
		if strings.HasPrefix(l, "#") {
			kc[l] = true
		} else if k := envKey.FindString(l); k != "" {
			pk[k[:len(k)-1]] = true
		}
	}
	val := map[string]string{}
	for _, l := range splitLines(string(old)) {
		if k := envKey.FindString(l); k != "" && pk[k[:len(k)-1]] {
			val[k[:len(k)-1]] = l[len(k):] // the last one wins
		}
	}
	var lo []string
	for _, l := range splitLines(tpl) {
		if k := envKey.FindString(l); k != "" {
			if v, ok := val[k[:len(k)-1]]; ok {
				l = k + v
			}
		}
		lo = append(lo, l)
	}
	if err := os.WriteFile(loc, []byte(strings.Join(lo, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	var out, buf []string
	for _, l := range splitLines(string(old)) {
		if strings.HasPrefix(l, "#") {
			buf = append(buf, l)
			continue
		}
		if k := envKey.FindString(l); k != "" && pk[k[:len(k)-1]] {
			for _, b := range buf {
				if !kc[b] {
					out = append(out, b)
				}
			}
			buf = nil
			continue
		}
		out = append(append(out, buf...), l)
		buf = nil
	}
	out = append(out, buf...)
	return os.WriteFile(f, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
