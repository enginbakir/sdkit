package hook

import (
	"os"

	"github.com/wecii/sdkit/internal/base"
	"github.com/wecii/sdkit/internal/stack"
)

func init() { Register(PostToolUse, []string{"Edit", "Write"}, "post-edit-fmt", postEditFmt) }

// stackOf is the stack named by STACK, go when unset (the documented default).
func stackOf(in *Input) stack.Stack {
	name := setting(in, "STACK")
	if name == "" {
		name = "go"
	}
	return stack.Get(name)
}

// postEditFmt formats the file that was just written with the stack's formatter.
func postEditFmt(in *Input) Decision {
	st := stackOf(in)
	if st == nil || in.Path == "" {
		return Decision{}
	}
	if fi, err := os.Stat(in.Path); err != nil || fi.IsDir() {
		return Decision{}
	}
	_ = st.Fmt(in.Path)
	return Decision{}
}

// setting reads a setting from the project files, then the environment.
func setting(in *Input, key string) string { return base.Getter(in.Settings().Raw)(key) }
