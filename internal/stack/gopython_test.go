package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGoPython(t *testing.T) {
	for file, want := range map[string]string{"go.mod": "go", "a/b/pyproject.toml": "python", "requirements.txt": "python", "node_modules/x/go.mod": ""} {
		d, p := t.TempDir(), ""
		p = filepath.Join(d, file)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o644)
		if Get("go").Detect(d) != (want == "go") || Get("python").Detect(d) != (want == "python") {
			t.Errorf("%s: want %q", file, want)
		}
	}
}
