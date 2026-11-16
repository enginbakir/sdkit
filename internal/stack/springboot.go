package stack

import "context"

func init() { Register(springBoot{}) }

// springBoot is jvm plus detection from the build file's content.
type springBoot struct{}

func (springBoot) Name() string { return "spring-boot" }

func (springBoot) Detect(root string) bool {
	return findBuildFile(root, true, `org\.springframework\.boot|spring-boot`)
}

func (springBoot) Tools() []Tool { return jvmTools(false) }

func (springBoot) IsSource(p string) bool { return jvmSource.MatchString(p) }

func (springBoot) Fmt(file string) error { return jvmFmt(file, false) }

func (springBoot) Steps(ctx context.Context, e Env, r Reporter) { jvmSteps(ctx, e, r) }
