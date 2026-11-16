package jira

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

const ok = `{"externalId":"s:T1","specId":"s","specVersion":1,"specSha":"` + "0123456789012345678901234567890123456789" + `","acceptanceCriteria":["a"],"risk":"low","dependencies":[],"scope":["x"],"parent":"P-1"}`

func TestContractAndToken(t *testing.T) {
	for in, want := range map[string]string{ok: "", `{"specId":"s"}`: "specVersion", "x": "no sdkit-task"} {
		if _, err := Validate(in); (err == nil) != (want == "") || err != nil && !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", in, err)
		}
	}
	r := testutil.TempRepo(t)
	_ = os.MkdirAll(filepath.Join(r, ".sdkit"), 0o755)
	_ = os.WriteFile(filepath.Join(r, ".sdkit", "sdkit.env"), []byte("JIRA_API_TOKEN=abc\n"), 0o644)
	if c := Load(r, func(string) string { return "" }); c.TokenErr == nil || c.TokenErr.(*Err).Code != 3 {
		t.Error("token in sdkit.env accepted")
	}
}

func TestUpsertErrorSaysWhatIsExpected(t *testing.T) {
	_, _, err := (&Config{}).Upsert("s:t1", "not json at all", nil)
	if err == nil || !strings.Contains(err.Error(), "render <tasks.json> <ref> --json") {
		t.Fatalf("the message must name what to pass: %v", err)
	}
}
