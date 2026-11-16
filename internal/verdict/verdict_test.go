package verdict

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestParse(t *testing.T) {
	pass, _ := os.ReadFile("../../testdata/golden/verdict/pass.in")
	for _, n := range []string{"pass", "malformed"} {
		in, _ := os.ReadFile("../../testdata/golden/verdict/" + n + ".in")
		r := Parse(string(in))
		testutil.Golden(t, "verdict/"+n, fmt.Sprintf("exit: %d\n%s\n", r.Code(), r.Line()))
	}
	for _, c := range []struct {
		name  string
		pairs []string
		code  int
		kind  string
	}{
		{"block", []string{"| PASS | verdict_test.go:12", "| FAIL | x", "VERDICT: PASS", "VERDICT: BLOCK"}, 1, ""},
		{"no evidence", []string{"| PASS | verdict_test.go:12", "| PASS | manual"}, 2, KindSemantic},
		{"empty evidence", []string{"| PASS | verdict_test.go:12", "| PASS |"}, 2, KindSemantic},
		{"wrong verdict", []string{"VERDICT: PASS", "VERDICT: BLOCK"}, 2, KindSemantic},
		{"red verify", []string{"result: green", "result: red"}, 2, KindSemantic},
		{"lowercase", []string{"| PASS |", "| pass |", "VERDICT: PASS", "**VERDICT:** pass"}, 0, ""},
		{"whitespace and bullets", []string{"- D1", " *   D1", "VERIFY\n", "**VERIFY**  \n"}, 0, ""},
		{"crlf", []string{"\n", "\r\n"}, 0, ""},
		{"extra text", []string{"DOD\n", "Here it is:\n```\nDOD\n", "PASS\n", "PASS\n```\nThanks\n"}, 0, ""},
		{"bad finding", []string{"- none", "- maybe | x"}, 2, KindFormat},
	} {
		r := Parse(strings.NewReplacer(c.pairs...).Replace(string(pass)))
		if r.Code() != c.code || r.Kind != c.kind {
			t.Errorf("%s: code %d kind %q (%s)", c.name, r.Code(), r.Kind, r.Reason)
		}
	}
}
