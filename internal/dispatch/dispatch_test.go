package dispatch

import (
	"path/filepath"
	"testing"
)

func TestOverlapRefused(t *testing.T) {
	l, tk := Ledger{Path: filepath.Join(t.TempDir(), "d.jsonl")}, "- [ ] T1 a | writes: src/\n- [ ] T2 b | writes: src/x.go\n"
	if _, err := l.Claim("T1", tk); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Claim("T2", tk); err == nil {
		t.Fatal("overlap claimed")
	}
}
