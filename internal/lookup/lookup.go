// Package lookup keeps scout answers ("where is X defined") for the task so the same question is not paid for twice. An answer is valid
// only for the code state it was given for: HEAD, the working-tree diff and every untracked file. Same key and state hashes as 1.x
// (sha1), so entries written by the bash script stay valid.
package lookup

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/gitx"
)

// C is a task's lookup cache. Root is the repository root, KitDir the project directory relative to it.
type C struct{ Root, KitDir, Slug string }

func sum(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }

// Key lowercases the question and collapses whitespace, so trivial rewording still hits.
func Key(q string) string { return sum([]byte(strings.Join(strings.Fields(strings.ToLower(q)), " "))) }

func (c C) dir() string { return filepath.Join(c.Root, c.KitDir, "work", c.Slug, "lookups") }

func (c C) git(args ...string) []byte {
	cmd := exec.Command("git", append([]string{"-C", c.Root}, args...)...)
	b, _ := cmd.Output()
	return b
}

// State is HEAD, the hash of the uncommitted diff and the hash of the names and contents of the untracked files (the kit's own work
// files are left out, or writing an answer would stale it).
func (c C) State() string {
	var u strings.Builder
	for _, f := range gitx.Lines(string(c.git("ls-files", "-o", "--exclude-standard", "--", ".", ":!"+c.KitDir+"/work", ":!"+c.KitDir+"/.sdd", ":!"+c.KitDir+"/.verify-cache"))) {
		u.WriteString(f + "\n")
		if st, err := os.Stat(filepath.Join(c.Root, f)); err == nil && st.Mode().IsRegular() {
			b, _ := os.ReadFile(filepath.Join(c.Root, f))
			u.WriteString(sum(b) + "\n")
		}
	}
	head := strings.TrimRight(string(c.git("rev-parse", "HEAD")), "\n")
	return head + " " + sum(c.git("diff", "HEAD")) + " " + sum([]byte(u.String()))
}

// Get returns the cached answer; ok is false on a miss (nothing cached, or the code moved on, which drops the entry).
func (c C) Get(q string) (string, bool) {
	f := filepath.Join(c.dir(), Key(q)+".md")
	b, err := os.ReadFile(f)
	if err != nil {
		return "", false
	}
	head, rest, _ := strings.Cut(string(b), "\n")
	if head != "state: "+c.State() {
		os.Remove(f)
		return "", false
	}
	return rest, true
}

// Put stores the answer for the current code state.
func (c C) Put(q, answer string) error {
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir(), Key(q)+".md"), []byte("state: "+c.State()+"\n"+answer), 0o644)
}

// Clear forgets the task's lookups.
func (c C) Clear() { os.RemoveAll(c.dir()) }
