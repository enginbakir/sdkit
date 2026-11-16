package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStaleAfterFileChange(t *testing.T) {
	k := K{t.TempDir(), ".sdkit", "t"}
	k.Put("k", "", []string{"a"}, "note")
	os.WriteFile(filepath.Join(k.Root, "a"), []byte("1"), 0o644)
	if _, err := k.Get("k", ""); err == nil {
		t.Fatal("fresh after the covered file changed")
	}
}
