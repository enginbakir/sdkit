package stack

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
)

func init() { Register(jvm{}) }

// jvm is Java or Kotlin on Maven or Gradle without Spring Boot (init probes
// spring-boot first, so jvm detects any build file, as in 1.x).
type jvm struct{}

var (
	jvmSource   = regexp.MustCompile(`\.(java|kt|kts)$`)
	jvmBuildRE  = regexp.MustCompile(`(^|/)(pom\.xml|build\.gradle|build\.gradle\.kts)$`)
	jvmBuildNms = []string{"pom.xml", "build.gradle", "build.gradle.kts"}
)

func (jvm) Name() string { return "jvm" }

func (jvm) Detect(root string) bool { return findBuildFile(root, false, "") }

func (jvm) Tools() []Tool { return jvmTools(true) }

func (jvm) IsSource(p string) bool { return jvmSource.MatchString(p) }

func (jvm) Fmt(file string) error { return jvmFmt(file, true) }

func (jvm) Steps(ctx context.Context, e Env, r Reporter) { jvmSteps(ctx, e, r) }

func jvmTools(kotlin bool) []Tool {
	t := []Tool{
		{true, "java", "install a JDK (see the build file for the version)"},
		{false, "mvn", "brew install maven (not needed when the project ships ./mvnw)"},
		{false, "gradle", "brew install gradle (not needed when the project ships ./gradlew)"},
		{false, "google-java-format", "brew install google-java-format (post-edit formatting is skipped without it)"},
	}
	if kotlin {
		t = append(t, Tool{false, "ktlint", "brew install ktlint (Kotlin post-edit formatting is skipped without it)"})
	}
	return t
}

// findBuildFile looks for a build file up to three levels down (files at depth
// <= 3), pruning node_modules, target and build (and .git for jvm). When
// needle is set the file must contain it (spring-boot detection).
func findBuildFile(root string, spring bool, needle string) bool {
	found := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		depth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "target", "build":
				return filepath.SkipDir
			case ".git":
				if !spring {
					return filepath.SkipDir
				}
			}
			if depth >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		for _, n := range jvmBuildNms {
			if d.Name() == n && d.Type().IsRegular() {
				if needle == "" {
					found = true
				} else if b, err := os.ReadFile(p); err == nil && regexp.MustCompile(needle).Match(b) {
					found = true
				}
			}
		}
		return nil
	})
	return found
}

func jvmFmt(file string, kotlin bool) error {
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	var args []string
	switch {
	case strings.HasSuffix(file, ".java"):
		args = []string{"google-java-format", "--replace", file}
	case kotlin && (strings.HasSuffix(file, ".kt") || strings.HasSuffix(file, ".kts")):
		args = []string{"ktlint", "-F", file}
	default:
		return nil
	}
	if bin, err := exec.LookPath(args[0]); err == nil {
		_ = exec.Command(bin, args[1:]...).Run() // never fails
	}
	return nil
}

func isFile(p string) bool { s, err := os.Stat(p); return err == nil && s.Mode().IsRegular() }

// jvmModuleOf is the nearest directory with a build file for a changed file ("" if none).
func jvmModuleOf(root, file string) string {
	has := func(dir string) bool {
		for _, n := range jvmBuildNms {
			if isFile(filepath.Join(root, filepath.FromSlash(dir), n)) {
				return true
			}
		}
		return false
	}
	for dir := path.Dir(file); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if has(dir) {
			return dir
		}
	}
	if has(".") {
		return "."
	}
	return ""
}

// wrapperOf is the absolute wrapper at the module or repo root, else the system tool.
func wrapperOf(root, mod, tool string) string {
	name, sys := "mvnw", "mvn"
	if tool == "gradle" {
		name, sys = "gradlew", "gradle"
	}
	if runtime.GOOS == "windows" {
		if name == "mvnw" {
			name += ".cmd"
		} else {
			name += ".bat"
		}
	}
	for _, w := range []string{filepath.Join(root, filepath.FromSlash(mod), name), filepath.Join(root, name)} {
		if s, err := os.Stat(w); err == nil && s.Mode().IsRegular() && (runtime.GOOS == "windows" || s.Mode()&0o111 != 0) {
			return w
		}
	}
	p, _ := exec.LookPath(sys)
	return p
}

// buildMentions: does the module's (or the root) build file mention the plugin?
func buildMentions(root, mod, word string) bool {
	for _, d := range []string{mod, "."} {
		for _, n := range jvmBuildNms {
			if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d), n)); err == nil &&
				strings.Contains(strings.ToLower(string(b)), word) {
				return true
			}
		}
	}
	return false
}

func jvmSteps(ctx context.Context, e Env, r Reporter) {
	seen := map[string]bool{}
	for _, f := range e.Source {
		if isFile(filepath.Join(e.Root, filepath.FromSlash(f))) {
			seen[jvmModuleOf(e.Root, f)] = true
		}
	}
	for _, f := range e.Changed {
		if jvmBuildRE.MatchString(f) && isFile(filepath.Join(e.Root, filepath.FromSlash(f))) {
			seen[jvmModuleOf(e.Root, f)] = true
		}
	}
	delete(seen, "")
	mods := make([]string, 0, len(seen))
	for m := range seen {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	integ := e.Settings.String("INTEGRATION_MODULE_GLOB")
	for _, mod := range mods {
		tool := e.Settings.String("JAVA_BUILD_TOOL")
		if tool != "maven" && tool != "gradle" {
			tool = "gradle"
			if isFile(filepath.Join(e.Root, filepath.FromSlash(mod), "pom.xml")) {
				tool = "maven"
			}
		}
		cmd := wrapperOf(e.Root, mod, tool)
		if cmd == "" {
			r.Warn("no %s wrapper or installation found, module %s skipped", tool, mod)
			continue
		}
		dir := filepath.Join(e.Root, filepath.FromSlash(mod))
		run := func(label string, mvn, gradle []string) {
			r.Step("module " + mod + ": " + label)
			args := gradle
			if tool == "maven" {
				args = mvn
			}
			if !r.Exec(ctx, dir, cmd, args...) {
				r.Fail("%s: %s", strings.SplitN(label, " ", 2)[0], mod)
			}
		}
		run("compile ("+tool+")", []string{"-q", "compile"}, []string{"-q", "classes"})
		if e.Fast {
			continue
		}
		if integ != "" && glob.Match(integ, mod) {
			continue // integration modules are compiled, never tested here
		}
		run("test", []string{"-q", "test"}, []string{"-q", "test"})
		if buildMentions(e.Root, mod, "spotless") {
			run("spotless", []string{"-q", "spotless:check"}, []string{"-q", "spotlessCheck"})
		}
		if buildMentions(e.Root, mod, "checkstyle") {
			run("checkstyle", []string{"-q", "checkstyle:check"}, []string{"-q", "checkstyleMain", "checkstyleTest"})
		}
	}
}
