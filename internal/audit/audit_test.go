package audit

import (
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/redact"
)

func TestGolden(t *testing.T) {
	b, _ := os.ReadFile("../../testdata/golden/security/audit.txt")
	s, p := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n== "), t.TempDir()+"/a"
	in, out, chain := strings.ReplaceAll(s[1][3:], "<>", ""), s[2][4:], s[3][6:]
	os.WriteFile(p, []byte(chain+"\n"), 0o644)
	n, br, _ := Verify(p)
	os.WriteFile(p, []byte(strings.Replace(chain, "gw.corp", "evil", 1)+"\n"), 0o644)
	_, br2, _ := Verify(p)
	if got := redact.Text(in + "\n"); got != out+"\n" || n != 3 || br != 0 || br2 != 1 {
		t.Errorf("redact %q (want %q); chain %d entries, broken %d, edited %d", got, out, n, br, br2)
	}
}
