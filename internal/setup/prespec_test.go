package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/testutil"
)

func TestPrespecRefused(t *testing.T) {
	r, n := testutil.TempRepo(t), func(string) {}
	_ = os.MkdirAll(filepath.Join(r, "prespec"), 0o755)
	_ = os.WriteFile(filepath.Join(r, "prespec", "a.md"), []byte("x"), 0o644)
	if PrespecCheck(r, "a", n, n) != 1 { // not ignored
		t.Fatal("non-ignored accepted")
	}
	_ = os.WriteFile(filepath.Join(r, ".gitignore"), []byte("/prespec/\n"), 0o644)
	_, _ = gitx.Run(r, "add", "-f", "prespec/a.md")
	if PrespecCheck(r, "a", n, n) != 1 { // tracked
		t.Fatal("tracked accepted")
	}
}

func TestParseGaps(t *testing.T) {
	text := "- G1 | DECISION | 2.3 | Which message on a server reject? | default: none | owner: Tech Lead\n" +
		"- G2 | DEFAULT | 4.1 | Trim spaces? | default: trim, keep password as typed | owner: analyst\n" +
		"- G3 | DEFAULT | 5 | Limit? | default: none | owner: unknown\n" +
		"- G2 | FACT | 6 | dup | default: none\n" +
		"SUMMARY: 1 decision, 0 fact, 2 default\n"
	gaps, problems := ParseGaps(text)
	if len(gaps) != 4 || len(problems) != 2 {
		t.Fatalf("%d gaps, problems %v", len(gaps), problems)
	}
	rep, ok := GapsReport(gaps[:2], nil)
	if !ok || !strings.Contains(rep, "1 to ask") || !strings.Contains(rep, "1 defaults") || !strings.Contains(rep, "trim, keep password as typed") {
		t.Fatalf("%s", rep)
	}
}
