package hook

import (
	"bytes"
	"context"
	"strings"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/verify"
)

func init() { Register(Stop, nil, "stop-verify", stopVerify) }

// stopVerify blocks "done" while the uncommitted changes fail the fast verify. A second Stop
// caused by its own block (stop_hook_active) must not loop forever.
func stopVerify(in *Input) Decision {
	if active, _ := in.Raw["stop_hook_active"].(bool); active {
		return Decision{}
	}
	var out bytes.Buffer
	code := 0
	if c := setting(in, "VERIFY_CMD"); c != "" {
		r, err := proc.RunShell(context.Background(), in.Root, nil, c+" --fast --since-head")
		if err != nil {
			return Decision{}
		}
		out.WriteString(r.Stdout + r.Stderr)
		code = r.Code
	} else {
		code = verify.Run(context.Background(), in.Root, verify.Options{Fast: true, SinceHead: true}, &out)
	}
	if code == 0 {
		return Decision{}
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	return Decision{Kind: Block, Reason: "verify failed, fix before finishing:\n" + strings.Join(lines, "\n")}
}
