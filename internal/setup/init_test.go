package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/testutil"
)

func TestInit(t *testing.T) {
	out := ""
	for _, file := range []string{"go.mod", "pom.xml", "requirements.txt", "README.md"} {
		d := t.TempDir()
		_ = os.WriteFile(filepath.Join(d, file), nil, 0o644)
		out += file + " -> " + detectStack(d) + "\n"
	}
	for _, o := range []InitOptions{{Hosts: []string{"claude"}, Stack: "go", skipOpenSpec: true}, {Hosts: []string{"codex"}, Stack: "go", skipOpenSpec: true}, {Env: "analyst", skipOpenSpec: true}} {
		d := testutil.TempRepo(t)
		New(d).Init(o)
		files, _ := gitx.Run(d, "ls-files", "-o")
		out += fmt.Sprintf("== %v %s\n%s\n", o.Hosts, o.Env, files)
	}
	testutil.Golden(t, "init/files", out)
}
