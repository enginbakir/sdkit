package testutil

import (
	"fmt"
	"testing"
)

func TestVersionGolden(t *testing.T) {
	out, code := RunKit(t, TempRepo(t), "", "version")
	Golden(t, "testutil/version", fmt.Sprintf("exit: %d\n%s", code, out))
}
