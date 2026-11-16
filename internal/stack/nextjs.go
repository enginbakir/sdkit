package stack

import (
	"context"
	"regexp"
	"strings"
)

func init() { Register(nextjs{}) }

// nextjs: Next.js and React apps (a package.json that depends on next).
type nextjs struct{}

var (
	nextSrc    = regexp.MustCompile(`\.(tsx?|jsx?|mjs|cjs)$`)
	nextFmtRe  = regexp.MustCompile(`\.(ts|tsx|js|jsx|mjs|cjs|json|css|md)$`)
	nextConfig = regexp.MustCompile(`(^|/)(package\.json|tsconfig[^/]*\.json)$`)
)

func (nextjs) Name() string { return "nextjs" }
func (nextjs) Detect(root string) bool {
	return nodeDetect(root, "next", "dependencies", "devDependencies")
}
func (nextjs) IsSource(p string) bool { return nextSrc.MatchString(p) }
func (nextjs) Fmt(file string) error  { return nodeFmt(file, nextFmtRe, false) }
func (nextjs) Tools() []Tool {
	return nodeTools(
		Tool{false, "pnpm", "npm i -g pnpm (needed when the repo has pnpm-lock.yaml)"},
		Tool{false, "yarn", "npm i -g yarn (needed when the repo has yarn.lock)"})
}

// Steps: per package of the changed files, typecheck; unless Fast also lint, test and (VERIFY_BUILD=1) next build.
func (nextjs) Steps(ctx context.Context, e Env, r Reporter) {
	var cfg []string
	for _, f := range e.Changed {
		if nextConfig.MatchString(f) {
			cfg = append(cfg, f)
		}
	}
	for _, pkg := range nodePackages(e.Root, append(append([]string{}, e.Source...), cfg...)) {
		r.Step("package " + pkg + ": typecheck")
		if !isFile(pkgDir(e, pkg) + "/tsconfig.json") {
			r.Warn("typecheck skipped in %s: no tsconfig.json", pkg)
		} else {
			nodeCheck(ctx, e, r, pkg, "typecheck", "typecheck", "tsc", "--noEmit")
		}
		if e.Fast {
			continue
		}

		r.Step("package " + pkg + ": lint")
		var files []string
		for _, f := range e.Source {
			if isFile(e.Root+"/"+f) && nodePkgOf(e.Root, f) == pkg {
				if pkg != "." {
					f = strings.TrimPrefix(f, pkg+"/")
				}
				files = append(files, f)
			}
		}
		nodeCheck(ctx, e, r, pkg, "lint", "lint", "eslint", files...)

		r.Step("package " + pkg + ": test")
		switch {
		case !nodeHasScript(e, pkg, "test"):
			r.Warn("no test script in %s", pkg)
		case nodeDefaultTest(e, pkg):
			r.Warn("test script in %s is the npm default, no tests", pkg)
		default:
			nodeCheck(ctx, e, r, pkg, "test", "test", "true")
		}

		if e.Settings != nil && e.Settings.String("VERIFY_BUILD") == "1" && nodeHasScript(e, pkg, "build") {
			r.Step("package " + pkg + ": next build")
			nodeCheck(ctx, e, r, pkg, "build", "build", "next", "build")
		}
	}
}
