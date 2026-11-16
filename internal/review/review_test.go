package review

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStatus(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "s1.md") // the document doubles as the empty findings array
	os.WriteFile(doc, []byte("[]"), 0o644)
	rec := func(v, iso string) string {
		Load(root).Begin("s1", doc, false)
		Load(root).Record("s1", v, iso, doc)
		return Status(root, "s1").State
	}
	got := fmt.Sprintln(Status(root, "s1").State, rec("NOT_RUN", "none"), rec("PASS", "none"), rec("PASS", "fresh"))
	os.WriteFile(doc, []byte("b"), 0o644)
	if got += Status(root, "s1").State; got != "NOT_RUN NOT_RUN NOT_RUN PASS\nstale" {
		t.Fatal(got)
	}
}
