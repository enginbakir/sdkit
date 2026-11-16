// Package knowledge stores task-scoped discovery notes ("how does X work") that stay valid exactly while every file they cover has the
// same content (sha256). A note is the writer's claim, never evidence: independent roles are refused.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/store"
)

// K is a task's notes. Root is the repository root, KitDir the project directory relative to it.
type K struct{ Root, KitDir, Slug string }

type meta struct {
	Title  string   `json:"title"`
	TS     string   `json:"ts"`
	Hashes []string `json:"hashes"` // "<sha256>  <file>", or "MISSING  <file>"
}

var (
	// ErrStale is a note whose covered files changed; the message names them.
	ErrStale = errors.New("stale")
	// ErrMiss is no note under the key.
	ErrMiss = errors.New("miss")
	// ErrRole is an independent role asking for a summary.
	ErrRole = errors.New("role")
	// ErrSecret is a body that looks like a secret.
	ErrSecret = errors.New("secret")
	secretRE  = regexp.MustCompile(`(?i)((api[_-]?key|secret|password|token)[[:space:]]*[:=][[:space:]]*[^[:space:]]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY|sk-[A-Za-z0-9]{16,})`)
	keyRE     = regexp.MustCompile(`^[A-Za-z0-9_.-]*$`)
)

// ValidKey reports whether key uses letters, digits and . _ - only.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

func (k K) dir() string { return filepath.Join(k.Root, k.KitDir, "work", k.Slug, "knowledge") }

func (k K) hash(f string) string {
	b, err := os.ReadFile(filepath.Join(k.Root, f))
	if err != nil {
		return "MISSING  " + f
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) + "  " + f
}

// Put stores the note for the files it covers; it returns how many.
func (k K) Put(key, title string, files []string, body string) (int, error) {
	for _, l := range strings.Split(body, "\n") {
		if secretRE.MatchString(l) {
			return 0, ErrSecret
		}
	}
	m := meta{Title: title, TS: time.Now().UTC().Format("2006-01-02T15:04:05Z")}
	if m.Title == "" {
		m.Title = key
	}
	for _, f := range files {
		m.Hashes = append(m.Hashes, k.hash(f))
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(k.dir(), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(k.dir(), key+".md"), []byte(strings.TrimRight(body, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}
	return len(files), store.WriteAtomic(filepath.Join(k.dir(), key+".json"), append(b, '\n'))
}

func (k K) load(key string) (meta, error) {
	var m meta
	b, err := os.ReadFile(filepath.Join(k.dir(), key+".json"))
	if err != nil {
		return m, fmt.Errorf("%w: no note '%s'", ErrMiss, key)
	}
	json.Unmarshal(b, &m)
	return m, nil
}

func (k K) stale(m meta) []string {
	var s []string
	for _, h := range m.Hashes {
		if _, f, _ := strings.Cut(h, "  "); k.hash(f) != h {
			s = append(s, f)
		}
	}
	return s
}

// Get returns the note when every covered file is unchanged. An independent role (verifier, reviewer, security-auditor, with or without -deep) is refused.
func (k K) Get(key, role string) (string, error) {
	switch strings.TrimSuffix(role, "-deep") {
	case "verifier", "reviewer", "security-auditor":
		return "", fmt.Errorf("%w: %s is an independent role; it reads the code and the diff itself, never a summary", ErrRole, role)
	}
	m, err := k.load(key)
	if err != nil {
		return "", err
	}
	if s := k.stale(m); len(s) > 0 {
		return "", fmt.Errorf("%w: changed or removed since the note was written: %s", ErrStale, strings.Join(s, " "))
	}
	b, _ := os.ReadFile(filepath.Join(k.dir(), key+".md"))
	return "UNTRUSTED SUMMARY (a claim by the role that wrote it, valid only for the file contents it was hashed against; not evidence)\n" + string(b), nil
}

// List is one line per note: key, covered file count, fresh|stale, title.
func (k K) List() []string {
	js, _ := filepath.Glob(filepath.Join(k.dir(), "*.json"))
	sort.Strings(js)
	var out []string
	for _, j := range js {
		key := strings.TrimSuffix(filepath.Base(j), ".json")
		m, _ := k.load(key)
		st := "fresh"
		if len(k.stale(m)) > 0 {
			st = "stale"
		}
		out = append(out, fmt.Sprintf("%s %d %s %s", key, len(m.Hashes), st, m.Title))
	}
	return out
}

// Clear forgets the task's notes.
func (k K) Clear() { os.RemoveAll(k.dir()) }
