package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoctor(t *testing.T) {
	for _, c := range []struct{ env, want string }{{"SDKIT_ENV=boss\n", "FAIL  SDKIT_ENV 'boss'"}, {"BASE_BRANCH=zz\n", "FAIL  BASE_BRANCH 'zz'"}, {"MAX_PARALLEL=0\n", "FAIL  MAX_PARALLEL '0'"}, {"", "FAIL  .sdkit/project.md missing"}} {
		d, out := t.TempDir(), &bytes.Buffer{}
		put(d+"/.sdkit/sdkit.env", []byte(c.env), 0o644)
		Doctor(d, func() *Setup { s := New(d); s.Out = out; return s }(), func(string) string { return "" })
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("missing %q in:\n%s", c.want, out)
		}
	}
}

func TestDoctorOpenSpecOnlyForAnalyst(t *testing.T) {
	for env, want := range map[string]bool{"": false, "SDKIT_ENV=developer\n": false, "SDKIT_ENV=analyst\n": true} {
		d, out := t.TempDir(), &bytes.Buffer{}
		put(d+"/.sdkit/sdkit.env", []byte(env), 0o644)
		Doctor(d, func() *Setup { s := New(d); s.Out = out; return s }(), func(string) string { return "" })
		if got := strings.Contains(out.String(), "openspec CLI"); got != want {
			t.Errorf("env %q: openspec checked = %v, want %v", env, got, want)
		}
	}
}
