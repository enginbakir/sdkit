package envfile

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

const keys = "PLAIN INDENT DQ SQ EXPORTED DEFAULTED FROM_ENV EMPTY TRAIL DQC REF REF2 UNDEF_REF OVER BARE_HASH SDKIT_ENV LOCAL_ONLY " +
	"CMDSUB SEMI OTHER AND UNQUOTED BACKTICK POSARG DOLLARS LD_PRELOAD SDKIT_FOO SPACED PIPE BSLASH"

func TestGoldens(t *testing.T) {
	for _, mode := range []string{"values", "report"} {
		var warn bytes.Buffer
		s := &Settings{Warn: &warn, Quiet: mode == "values", Getenv: func(k string) string { return map[string]string{"FROM_ENV": "fromenv"}[k] }}
		for _, f := range [][2]string{{"forms.env", "sdkit.env"}, {"forms.local.env", "sdkit.local.env"}} {
			b, err := os.ReadFile("../../testdata/golden/envfile/" + f[0])
			if err != nil {
				t.Fatal(err)
			}
			s.LoadBytes(f[1], b)
		}
		out := warn.String()
		if mode == "values" {
			for _, k := range strings.Fields(keys) {
				if v, ok := s.Raw(k); ok {
					out += fmt.Sprintf("%s=[%s]\n", k, v)
				} else {
					out += k + "=<unset>\n"
				}
			}
		}
		testutil.Golden(t, "envfile/"+mode, fmt.Sprintf("exit: 0\n%sbad=%d\n", out, s.Bad))
	}
}

func TestForms(t *testing.T) {
	u16 := "\xff\xfeA\x00=\x001\x00\n\x00"
	for _, c := range []struct{ in, want string }{
		{"A=1", "1"}, {"A=\"x y\"", "x y"}, {"A='x $y'", "x $y"}, {"export A=1", "1"}, {`: "${A:=d}"`, "d"}, {"A=1 # c", "1"},
		{"A=1\r\n", "1"}, {"\xef\xbb\xbfA=1\n", "1"}, {u16, "1"}, {"B=2\nA=$B-${B}", "2-2"},
		{"A=$(id)", ""}, {"A=1;B=2", ""}, {"A=1 && B", ""}, {"ls", ""}, {"A=x y", ""}, {"A=$1", ""}, {"PATH=/x", ""}, {"LD_PRELOAD=x", ""}, {"SDKIT_X=1", ""},
	} {
		s := &Settings{Quiet: true}
		s.LoadBytes("f", []byte(c.in))
		if got, _ := s.Raw("A"); got != c.want {
			t.Errorf("%q: A=%q, want %q", c.in, got, c.want)
		}
	}
	if s := New(); s.String("SECRET_GLOBS") != ".env*,*credentials*,*.pem" || len(s.List("SECRET_GLOBS")) != 3 || !s.Bool("MEMORY") || s.Int("ROUTE_MAX_RETRIES", 0) != 2 {
		t.Error("embedded defaults not read")
	}
}
