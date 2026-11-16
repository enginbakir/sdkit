package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJVMDetect(t *testing.T) {
	for body, want := range map[string][2]bool{"": {false, false}, "<project/>": {true, false}, "spring-boot": {true, true}} {
		d := t.TempDir()
		if body != "" {
			os.WriteFile(filepath.Join(d, "pom.xml"), []byte(body), 0o644)
		}
		if got := [2]bool{(jvm{}).Detect(d), (springBoot{}).Detect(d)}; got != want {
			t.Errorf("%q: %v", body, got)
		}
	}
}
