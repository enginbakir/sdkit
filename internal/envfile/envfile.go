// Package envfile parses the kit's settings files (sdkit.env, sdkit.local.env).
// They are data, never executed: see "Settings" in docs/go-port/02-contracts.md.
// The accepted line forms and refused keys mirror scripts/env.sh.
package envfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	kit "github.com/wecii/sdkit"
)

// Settings holds the values read so far. The zero value works (no defaults).
type Settings struct {
	vals map[string]string
	defs map[string]string
	// Bad counts the lines ignored so far.
	Bad int
	// Warn receives the "ignored" report; nil means os.Stderr. Quiet silences it.
	Warn  io.Writer
	Quiet bool
	// Getenv is consulted only by the `: "${KEY:=default}"` form (a process
	// variable wins there, as in env.sh). Nil means os.Getenv.
	Getenv func(string) string
}

// New returns Settings with the defaults of the embedded templates/sdkit.env
// and sdkit.local.env, and Quiet set from SDKIT_ENV_QUIET=1.
func New() *Settings {
	d := &Settings{Quiet: true, Getenv: func(string) string { return "" }}
	for _, f := range []string{"templates/sdkit.env", "templates/sdkit.local.env"} {
		b, err := kit.FS.ReadFile(f)
		if err == nil {
			d.LoadBytes(f, b)
		}
	}
	defs := map[string]string{}
	for k, v := range d.vals {
		v = strings.ReplaceAll(v, "__KIT_DIR__", ".sdkit")
		if !strings.Contains(v, "__") { // __BASE_BRANCH__, __STACK__: filled in by init, no default
			defs[k] = v
		}
	}
	return &Settings{defs: defs, Quiet: os.Getenv("SDKIT_ENV_QUIET") == "1"}
}

var (
	reAssign  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	reDefault = regexp.MustCompile("^:[[:space:]]+\"?\\$\\{([A-Za-z_][A-Za-z0-9_]*):=([^$}\"`\\\\[:space:]]*)\\}\"?[[:space:]]*$")
	reSQ      = regexp.MustCompile(`^'([^']*)'[[:space:]]*(#.*)?$`)
	reDQ      = regexp.MustCompile("^\"([^\"\\\\`]*)\"[[:space:]]*(#.*)?$")
	reBare    = regexp.MustCompile("^([^[:space:];&|<>()\\\\`\"']*)([[:space:]]+#.*)?[[:space:]]*$")
	reName    = regexp.MustCompile(`^(?:([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)\})`)
)

func refused(k string) bool {
	if k == "SDKIT_ENV" {
		return false
	}
	switch k {
	case "PATH", "IFS", "HOME", "SHELL", "ENV", "BASH_ENV", "CDPATH", "GLOBIGNORE", "PS1", "PS2", "PS4", "PROMPT_COMMAND",
		"SHELLOPTS", "BASHOPTS", "POSIXLY_CORRECT", "FUNCNEST", "TMPDIR", "KIT_DIR", "KIT_ENV", "PROJECT_DIR", "PLUGIN_ROOT":
		return true
	}
	for _, p := range []string{"BASH_", "LD_", "DYLD_", "SDKIT_"} {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// expand replaces $NAME and ${NAME} with an earlier setting; any other $ fails.
func (s *Settings) expand(t string) (string, bool) {
	var out strings.Builder
	for {
		i := strings.IndexByte(t, '$')
		if i < 0 {
			return out.String() + t, true
		}
		out.WriteString(t[:i])
		m := reName.FindStringSubmatch(t[i+1:])
		if m == nil {
			return "", false
		}
		out.WriteString(s.vals[m[1]+m[2]])
		t = t[i+1+len(m[0]):]
	}
}

func isSpace(r byte) bool { return r == ' ' || (r >= '\t' && r <= '\r') }

// LoadFile reads one settings file; a missing file is not an error. Reported
// names are the file's base name.
func (s *Settings) LoadFile(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s.LoadBytes(filepath.Base(path), b)
	return nil
}

// LoadBytes parses one file's content (UTF-8, with or without BOM, or UTF-16
// with BOM; CRLF tolerated); name is used in the report.
func (s *Settings) LoadBytes(name string, data []byte) {
	if s.vals == nil {
		s.vals = map[string]string{}
	}
	text := decode(data)
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimLeftFunc(strings.TrimSuffix(line, "\r"), func(r rune) bool { return r < 128 && isSpace(byte(r)) })
		if line == "" || line[0] == '#' {
			continue
		}
		if !s.parseLine(line) {
			s.Bad++
			if !s.Quiet {
				w := s.Warn
				if w == nil {
					w = os.Stderr
				}
				fmt.Fprintf(w, "sdkit: %s:%d: ignored (unsupported syntax; see docs/upgrading.md, \"Settings files are parsed\")\n", name, n+1)
			}
		}
	}
}

func (s *Settings) parseLine(line string) bool {
	if m := reDefault.FindStringSubmatch(line); m != nil {
		if refused(m[1]) {
			return false
		}
		if s.vals[m[1]] == "" {
			genv := s.Getenv
			if genv == nil {
				genv = os.Getenv
			}
			if e := genv(m[1]); e != "" {
				s.vals[m[1]] = e
			} else {
				s.vals[m[1]] = m[2]
			}
		}
		return true
	}
	if strings.HasPrefix(line, "export") && len(line) > 6 && isSpace(line[6]) {
		line = strings.TrimLeftFunc(line[6:], func(r rune) bool { return r < 128 && isSpace(byte(r)) })
	}
	m := reAssign.FindStringSubmatch(line)
	if m == nil || refused(m[1]) {
		return false
	}
	val := m[2]
	switch {
	case reSQ.MatchString(val):
		val = reSQ.FindStringSubmatch(val)[1]
	case reDQ.MatchString(val):
		v, ok := s.expand(reDQ.FindStringSubmatch(val)[1])
		if !ok {
			return false
		}
		val = v
	case reBare.MatchString(val):
		v, ok := s.expand(reBare.FindStringSubmatch(val)[1])
		if !ok {
			return false
		}
		val = v
	default:
		return false
	}
	s.vals[m[1]] = val
	return true
}

func decode(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
		le := b[0] == 0xFF
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if le {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i+1])|uint16(b[i])<<8)
			}
		}
		return string(utf16.Decode(u))
	}
	return strings.TrimPrefix(string(b), "\xef\xbb\xbf")
}

// Raw returns the value read from files (or won by the environment), without defaults.
func (s *Settings) Raw(key string) (string, bool) { v, ok := s.vals[key]; return v, ok }

// Lookup returns the value of key, falling back to the embedded default.
func (s *Settings) Lookup(key string) (string, bool) {
	if v, ok := s.vals[key]; ok {
		return v, true
	}
	v, ok := s.defs[key]
	return v, ok
}

// String returns the value of key, or "" when unknown.
func (s *Settings) String(key string) string { v, _ := s.Lookup(key); return v }

// Bool is true for 1, on, true, yes (any case); false for anything else.
func (s *Settings) Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(s.String(key))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// Int returns key as an integer, or def when empty or not a number.
func (s *Settings) Int(key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s.String(key)))
	if err != nil {
		return def
	}
	return n
}

// List splits a comma list, trimming blanks and dropping empty entries.
func (s *Settings) List(key string) []string {
	var out []string
	for _, p := range strings.Split(s.String(key), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Map returns every setting (defaults overlaid by what was read); a copy.
func (s *Settings) Map() map[string]string {
	m := make(map[string]string, len(s.defs)+len(s.vals))
	for k, v := range s.defs {
		m[k] = v
	}
	for k, v := range s.vals {
		m[k] = v
	}
	return m
}
