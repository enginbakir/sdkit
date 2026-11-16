package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeDetect(t *testing.T) {
	for pkg, want := range map[string][2]bool{`{"dependencies":{"next":"14"}}`: {true, false}, `{"devDependencies":{"next":"1"}}`: {true, false},
		`{"dependencies":{"react-native":"0.7"}}`: {false, true}, `{"peerDependencies":{"react-native":"*"}}`: {false, true}, `{}`: {false, false}} {
		d := t.TempDir()
		_ = os.MkdirAll(filepath.Join(d, "app"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "app", "package.json"), []byte(pkg), 0o644)
		if (nextjs{}).Detect(d) != want[0] || (reactNative{}).Detect(d) != want[1] {
			t.Errorf("%s", pkg)
		}
	}
}
