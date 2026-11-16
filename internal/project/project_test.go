package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestResolveAndLoad(t *testing.T) {
	repo := testutil.TempRepo(t)
	if p := ResolveWith(repo, ""); p.Found || p.KitDir != ".sdkit" {
		t.Fatalf("empty repo: %+v", p)
	}
	d := filepath.Join(repo, ".sdkit")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "sdkit.env"), []byte("STACK=node\nSDD_MODE=free\n"), 0o644)
	_ = os.WriteFile(filepath.Join(d, "sdkit.local.env"), []byte("SDD_MODE=strict\n"), 0o644)
	t.Setenv("KIT_DIR", "")
	s, p, err := Load(repo)
	if err != nil || !p.Found || s.String("STACK") != "node" || s.String("SDD_MODE") != "strict" {
		t.Fatalf("%v %+v", err, p)
	}
}
