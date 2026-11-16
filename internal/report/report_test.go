package report

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/testutil"
)

func TestReport(t *testing.T) { // golden: fresh tokens 18,300 (the duplicate ledger line is one run), saving = routing counterfactual + avoided run, N/A where data is missing
	d, out := t.TempDir(), &bytes.Buffer{}
	_ = os.CopyFS(filepath.Join(d, ".sdkit"), os.DirFS("../../testdata/golden/report/proj"))
	e := New(d).To(out)
	for _, a := range []string{"finalize --status completed --slug full", "finalize --status failed --slug na", "finalize --status completed --slug full", "task full --detailed", "task na --detailed", "session s1", "session s2 --markdown", "history"} {
		fmt.Fprintf(out, "=== report %s\n", a)
		e.Run(strings.Fields(a))
	}
	testutil.Golden(t, "report/report", out.String())
}
