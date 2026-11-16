package proc

import (
	"context"
	"fmt"
	"testing"
)

func TestShellExitCode(t *testing.T) {
	if r, err := RunShell(context.Background(), "", nil, "exit 3"); err != nil || r.Code != 3 {
		t.Fatalf("code=%d err=%v", r.Code, err)
	}
}

func TestShellArgv(t *testing.T) {
	for _, c := range [][3]string{{`C:\Program Files\PowerShell\7\pwsh.exe`, "windows", `[C:\Program Files\PowerShell\7\pwsh.exe -NoProfile -Command]`},
		{"CMD.EXE", "windows", "[CMD.EXE /C]"}, {"bash", "windows", "[bash -c]"}, {"pwsh", "linux", "[pwsh -c]"}, {"fish", "windows", "[fish -c]"}} {
		if got := fmt.Sprint(shellArgv(c[0], c[1])); got != c[2] {
			t.Errorf("%v: %s", c, got)
		}
	}
}
