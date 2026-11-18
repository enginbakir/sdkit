// Package project finds the repository root and the project directory
// (.sdkit) and loads its settings.
package project

import (
	"os"
	"path/filepath"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
)

// Project is where a repo keeps its kit files.
type Project struct {
	Root    string // repository root
	KitDir  string // project directory relative to Root (".sdkit" unless $KIT_DIR names another that has sdkit.env)
	EnvFile string // settings file name inside KitDir
	Found   bool   // false when no KitDir holds an EnvFile yet
	// Task is the task whose ledger this process's token usage belongs to, named by $SDKIT_TASK ("" when unset). A session that works
	// on one named thing (a spec id) says so here; without it the kit picks the open task, else the newest work state, else _misc.
	Task string
}

// Dir is the absolute project directory.
func (p Project) Dir() string { return filepath.Join(p.Root, p.KitDir) }

// Root returns the git top level of dir.
func Root(dir string) (string, error) { return gitx.Top(dir) }

// Resolve finds the project directory of root: $KIT_DIR (when set) then .sdkit,
// each needing sdkit.env. Pre-1.0 directories are not looked at (D2).
func Resolve(root string) Project {
	p := ResolveWith(root, os.Getenv("KIT_DIR"))
	p.Task = os.Getenv("SDKIT_TASK")
	return p
}

// ResolveWith is Resolve with the KIT_DIR value given.
func ResolveWith(root, kitDir string) Project {
	for _, d := range []string{kitDir, ".sdkit"} {
		if d == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, d, "sdkit.env")); err == nil && !st.IsDir() {
			return Project{Root: root, KitDir: d, EnvFile: "sdkit.env", Found: true}
		}
	}
	if kitDir == "" {
		kitDir = ".sdkit"
	}
	return Project{Root: root, KitDir: kitDir, EnvFile: "sdkit.env"}
}

// Load resolves the project of root and reads sdkit.env then sdkit.local.env
// (the second wins) over the embedded defaults.
func Load(root string) (*envfile.Settings, Project, error) {
	p := Resolve(root)
	s := envfile.New()
	for _, f := range []string{p.EnvFile, "sdkit.local.env"} {
		if err := s.LoadFile(filepath.Join(p.Dir(), f)); err != nil {
			return s, p, err
		}
	}
	return s, p, nil
}

// KitFile reads a file of the kit data embedded in the binary (core/, skills/, templates/ ...).
func KitFile(name string) ([]byte, error) { return kit.FS.ReadFile(name) }
