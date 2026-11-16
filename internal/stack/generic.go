package stack

import "context"

func init() { Register(generic{}) }

// generic is the fallback when no other stack is detected: the project supplies
// VERIFY_FAST_CMD and VERIFY_TEST_CMD in sdkit.env.
type generic struct{}

func (generic) Name() string         { return "generic" }
func (generic) Detect(string) bool   { return false }
func (generic) Tools() []Tool        { return nil }
func (generic) IsSource(string) bool { return true }
func (generic) Fmt(string) error     { return nil }

func (generic) Steps(ctx context.Context, e Env, r Reporter) {
	fast, tests := e.Settings.String("VERIFY_FAST_CMD"), e.Settings.String("VERIFY_TEST_CMD")
	if fast != "" {
		r.Step("fast check")
		if !r.Shell(ctx, e.Root, fast) {
			r.Fail("VERIFY_FAST_CMD")
		}
	}
	if e.Fast {
		return
	}
	if tests != "" {
		r.Step("tests")
		if !r.Shell(ctx, e.Root, tests) {
			r.Fail("VERIFY_TEST_CMD")
		}
	}
	if fast == "" && tests == "" {
		r.Warn("no VERIFY_FAST_CMD / VERIFY_TEST_CMD set in sdkit.env, nothing was checked (only verify.d policies ran)")
	}
}
