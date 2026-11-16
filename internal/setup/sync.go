package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wecii/sdkit/internal/store"
)

// syncEntry is one manifest line: dest, kit source, sha of what the kit installed.
type syncEntry struct{ dest, src, sha string }

func (s *Setup) syncManifest() string { return filepath.Join(s.Root, s.KitDir, ".scaffold-manifest") }

func (s *Setup) syncReadManifest() (out []syncEntry) {
	b, _ := os.ReadFile(s.syncManifest())
	for _, l := range splitLines(string(b)) {
		if f := strings.Split(l, "\t"); len(f) >= 2 && f[0] != "" {
			e := syncEntry{dest: f[0], src: f[1]}
			if len(f) > 2 {
				e.sha = f[2]
			}
			out = append(out, e)
		}
	}
	return out
}

// Sync refreshes the kit-owned copies listed in the manifest without clobbering local edits. With check
// nothing is written and the result is false when a copy is missing or differs (CI drift check). A copy
// edited locally and changed upstream is a conflict, kept as is unless Force. It returns true when in sync.
func (s *Setup) Sync(check bool) (bool, error) {
	if ResolveKit(s.Root) == "" {
		s.say("no sdkit.env found in .sdkit/; run sdkit init first")
		return false, nil
	}
	if _, err := os.Stat(s.syncManifest()); err != nil {
		s.say("no manifest at %s; run sdkit init first", s.syncManifest())
		return false, nil
	}
	old, _ := os.ReadFile(filepath.Join(s.Root, s.KitDir, ".scaffold-version"))
	s.say("scaffold %s -> %s", strings.TrimSpace(string(old)), s.Version)
	ok := true
	if !check {
		_ = os.WriteFile(s.manifestNew(), nil, 0o644)
	}
	for _, e := range s.syncReadManifest() {
		text, err := s.kitFile(e.src)
		if err != nil {
			s.say("  gone   %s (removed upstream; left in place)", e.dest)
			continue
		}
		text = s.renderManaged(text, e.dest)
		cur, rerr := os.ReadFile(s.abs(e.dest))
		if check {
			if rerr != nil {
				s.say("  missing %s", e.dest)
				ok = false
			} else if string(cur) != text {
				s.say("  drift   %s differs from kit %s", e.dest, s.Version)
				ok = false
			}
			continue
		}
		perm := os.FileMode(0o644)
		if strings.HasPrefix(e.dest, ".githooks/") || strings.HasSuffix(e.dest, ".sh") {
			perm = 0o755
		}
		switch {
		case rerr != nil:
			if err := put(s.abs(e.dest), []byte(text), perm); err != nil {
				return false, err
			}
			s.say("  added  %s", e.dest)
		case string(cur) == text:
		case shaFile(s.abs(e.dest)) == e.sha || s.Force:
			if err := put(s.abs(e.dest), []byte(text), perm); err != nil {
				return false, err
			}
			_ = os.Chmod(s.abs(e.dest), perm)
			s.say("  update %s", e.dest)
		default:
			s.say("  CONFLICT %s edited locally and changed upstream; rerun with --force to take upstream", e.dest)
			_ = store.AppendLine(s.manifestNew(), e.dest+"\t"+e.src+"\t"+e.sha)
			continue
		}
		if err := s.recordManaged(e.dest, e.src); err != nil {
			return false, err
		}
	}
	s.say("shared files (kit part only; yours is never touched)")
	if err := s.RefreshShared(check); err != nil {
		ok = false
	}
	if check {
		if ok {
			s.say("in sync with kit %s", s.Version)
		} else {
			s.say("drift found: run sdkit sync (or upgrade) and commit the result")
		}
		return ok, nil
	}
	if err := s.FinishManifest(); err != nil {
		return false, err
	}
	s.say("sync done")
	return true, nil
}
