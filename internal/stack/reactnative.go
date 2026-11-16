package stack

import (
	"context"
	"regexp"
	"runtime"
)

func init() { Register(reactNative{}) }

// reactNative: bare and Expo apps (a package.json that depends on react-native).
// Steps never build native code (no prebuild, gradle, xcodebuild, pod install, metro).
type reactNative struct{}

var (
	rnSrc    = regexp.MustCompile(`\.(tsx?|jsx?)$`)
	rnNative = regexp.MustCompile(`(^|/)(ios|android)/|(^|/)(app\.json|app\.config\.[a-z]+|Podfile|build\.gradle)$`)
)

func (reactNative) Name() string           { return "react-native" }
func (reactNative) IsSource(p string) bool { return rnSrc.MatchString(p) }
func (reactNative) Fmt(file string) error  { return nodeFmt(file, rnSrc, true) }
func (reactNative) Detect(root string) bool {
	return nodeDetect(root, "react-native", "dependencies", "devDependencies", "peerDependencies")
}

func (reactNative) Tools() []Tool {
	t := nodeTools(
		Tool{false, "prettier", "install it as a devDependency (format-on-edit is skipped without it)"},
		Tool{false, "watchman", "brew install watchman (Metro and jest-haste-map file watching)"},
		Tool{false, "adb", "install Android Studio or the platform-tools and put adb on PATH to run android/"})
	if runtime.GOOS == "darwin" {
		t = append(t, Tool{false, "xcrun", "install Xcode and its command line tools (xcode-select --install) to build and run ios/"})
	}
	return t
}

// Steps: warn on native/config changes; per package of the changed sources, typecheck; unless Fast also lint
// (prettier --check when there is no eslint) and tests (package script, else jest on the related files).
func (reactNative) Steps(ctx context.Context, e Env, r Reporter) {
	for _, f := range e.Changed {
		if rnNative.MatchString(f) {
			r.Warn("native/config files changed (ios/android/app config): build and smoke-test on a device or simulator, verify does not")
			break
		}
	}
	for _, pkg := range nodePackages(e.Root, e.Source) {
		r.Step("package " + pkg + ": typecheck")
		if isFile(pkgDir(e, pkg) + "/tsconfig.json") {
			nodeCheck(ctx, e, r, pkg, "typecheck", "typecheck", "tsc", "--noEmit")
		} else {
			r.Warn("typecheck skipped in %s: no tsconfig.json", pkg)
		}
		if e.Fast {
			continue
		}

		var files []string // absolute paths
		for _, f := range e.Source {
			if isFile(e.Root+"/"+f) && nodePkgOf(e.Root, f) == pkg {
				files = append(files, e.Root+"/"+f)
			}
		}

		r.Step("package " + pkg + ": lint")
		if !nodeHasScript(e, pkg, "lint") && localBin(e, pkg, "eslint") == "" && localBin(e, pkg, "prettier") != "" {
			// no eslint: the formatting check is the only style gate there is
			if _, ok := nodeExec(ctx, e, r, pkg, "prettier", append([]string{"--check"}, files...)...); !ok {
				r.Fail("prettier --check: %s (run prettier --write)", pkg)
			}
		} else {
			nodeCheck(ctx, e, r, pkg, "lint", "lint", "eslint", files...)
		}

		r.Step("package " + pkg + ": test")
		switch {
		case !nodeInstalled(e, pkg):
			r.Warn("tests skipped in %s: dependencies not installed", pkg)
		case nodeHasScript(e, pkg, "test"):
			if nodeDefaultTest(e, pkg) {
				r.Warn("no tests in %s (npm default test script)", pkg)
			} else if !nodeRun(ctx, e, r, pkg, "test") {
				r.Fail("test: %s", pkg)
			}
		case pkgHasDep(pkgDir(e, pkg), "jest", "dependencies", "devDependencies"):
			if _, ok := nodeExec(ctx, e, r, pkg, "jest", append([]string{"--findRelatedTests"}, append(files, "--passWithNoTests")...)...); !ok {
				r.Fail("jest: %s", pkg)
			}
		default:
			r.Warn("no tests in %s: no 'test' script and no jest dependency", pkg)
		}
	}
}
