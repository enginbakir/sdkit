package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/store"
)

// Files that hold both the kit's lines and the project's own (AGENTS.md, CLAUDE.md, settings.json, hooks.json,
// config.toml). The kit rewrites only its marked block (text files) or its own keys and entries (JSON files);
// everything else is the project's. Every entry is recorded in <project-dir>/.scaffold-shared for sync/doctor.

// SharedFileName is the record of shared files inside the project directory.
const SharedFileName = ".scaffold-shared"

// ErrDrift is returned by check mode when the kit part of a file is out of date.
var ErrDrift = errors.New("kit part is out of date")

var errFailed = errors.New("shared file not applied") // already reported with a FAIL line

func (s *Setup) markers(style string) (begin, end string) {
	if style == "md" {
		return "<!-- spec-driven-kit:begin v" + s.Version + ". Managed by the kit: do not edit between the markers; write your own text outside them. -->", "<!-- spec-driven-kit:end -->"
	}
	return "# spec-driven-kit:begin v" + s.Version + ". Managed by the kit: do not edit between the markers; write your own settings outside them.", "# spec-driven-kit:end"
}

var agentsSection = regexp.MustCompile(`(?m)^\[agents\]`)

// blockFilter: the Codex config may define [agents] already; defining it twice is a TOML error, so the block leaves it out.
func blockFilter(dest, body string, current string) string {
	if dest != ".codex/config.toml" || !agentsSection.MatchString(current) {
		return body
	}
	in := false // [agents] inside the kit block itself
	for _, l := range splitLines(current) {
		switch {
		case strings.Contains(l, "spec-driven-kit:begin"):
			in = true
		case strings.Contains(l, "spec-driven-kit:end"):
			in = false
		case in && strings.HasPrefix(l, "[agents]"):
			return body
		}
	}
	var out []string
	skip := false
	for _, l := range splitLines(body) {
		if strings.HasPrefix(l, "[agents]") {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(l, "[") {
			skip = false
		}
		if !skip {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// ApplyBlock writes (or, with check, only compares) the marked kit block of dest from src; style is md or hash.
// header starts a new file. Text outside the markers is never touched.
func (s *Setup) ApplyBlock(dest, src, style string, check bool, header string) error {
	text, err := s.kitFile(src)
	if err != nil {
		return err
	}
	p := s.abs(dest)
	cur, rerr := os.ReadFile(p)
	old := string(cur)
	begin, end := s.markers(style)
	body := s.render(text)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	blk := begin + "\n" + blockFilter(dest, body, old) + end + "\n"
	switch {
	case rerr != nil:
		if check {
			return ErrDrift
		}
		if header != "" {
			blk = header + "\n\n" + blk
		}
		s.say("  wrote  %s (new; your own text goes outside the kit block)", dest)
		return put(p, []byte(blk), 0o644)
	case strings.Contains(old, "spec-driven-kit:begin"):
		if !strings.Contains(old, "spec-driven-kit:end") {
			s.say("  FAIL   %s has a spec-driven-kit:begin marker but no :end marker; fix it by hand", dest)
			return errFailed
		}
		var out strings.Builder
		skip := false
		for _, l := range splitLines(old) {
			switch {
			case strings.Contains(l, "spec-driven-kit:begin"):
				out.WriteString(blk)
				skip = true
			case strings.Contains(l, "spec-driven-kit:end"):
				skip = false
			case !skip:
				out.WriteString(l + "\n")
			}
		}
		if out.String() == old {
			if !check {
				s.say("  ok     %s (kit block current)", dest)
			}
			return nil
		}
		if check {
			return ErrDrift
		}
		s.say("  update %s (kit block only)", dest)
		return os.WriteFile(p, []byte(out.String()), 0o644)
	}
	if check {
		return ErrDrift
	}
	if old != "" && !strings.HasSuffix(old, "\n") {
		old += "\n"
	}
	s.say("  append %s (kit block added after your content; remove older kit-generated text from this file once, if any)", dest)
	return os.WriteFile(p, []byte(old+"\n"+blk), 0o644)
}

// MergeJSON adds the kit's keys and entries from src to the JSON file dest and keeps everything else.
// kind: claude-settings (enabledPlugins, extraKnownMarketplaces, permissions, kit hooks), hooks-grouped (Gemini),
// hooks-flat (Cursor). It is idempotent; with check nothing is written and ErrDrift means out of date.
func (s *Setup) MergeJSON(kind, dest, src string, check bool) error {
	fail := func(format string, a ...any) error { s.say("  FAIL   "+format, a...); return errFailed }
	text, err := s.kitFile(src)
	if err != nil {
		return err
	}
	var extra []string
	if kind == "claude-settings" {
		url := s.MarketplaceURL
		if url == "" {
			url = "https://example.invalid/spec-driven-kit.git"
		}
		text = strings.ReplaceAll(text, "__MARKETPLACE_URL__", url)
		if b, err := s.kitFile("stacks/" + s.stack() + "/permissions.txt"); err == nil {
			extra = splitLines(b)
		}
	}
	kv, err := parseJSON([]byte(s.render(text)))
	kd, ok := kv.(*obj)
	if err != nil || !ok {
		return fail("%s renders to invalid JSON", src)
	}
	if kind == "claude-settings" {
		if err := addPermissions(kd, extra); err != nil {
			return fail("%s renders to invalid JSON", src)
		}
	}
	p := s.abs(dest)
	doc := newObj()
	cur, rerr := os.ReadFile(p)
	if rerr == nil {
		v, err := parseJSON(cur)
		o, ok := v.(*obj)
		if err != nil || !ok {
			return fail("%s is not valid JSON; fix it, then re-run", dest)
		}
		doc = o
	}
	if err := mergeKind(kind, doc, kd); err != nil {
		return fail("could not merge %s", dest)
	}
	if rerr == nil {
		if v, _ := parseJSON(cur); canon(v) == canon(doc) {
			if !check {
				s.say("  ok     %s (kit entries current)", dest)
			}
			return nil
		}
	}
	if check {
		return ErrDrift
	}
	if rerr == nil {
		s.say("  merge  %s (kit entries only; your entries are kept)", dest)
	} else {
		s.say("  wrote  %s", dest)
	}
	return put(p, marshal(doc, false), 0o644)
}

// ApplyShared dispatches on kind: block-md, block-hash, claude-settings, hooks-grouped, hooks-flat.
func (s *Setup) ApplyShared(kind, dest, src string, check bool) error {
	switch kind {
	case "block-md":
		name := s.ProjectName
		if name == "" {
			name = filepath.Base(s.Root)
		}
		return s.ApplyBlock(dest, src, "md", check, "# "+name)
	case "block-hash":
		return s.ApplyBlock(dest, src, "hash", check, "")
	case "claude-settings", "hooks-grouped", "hooks-flat":
		return s.MergeJSON(kind, dest, src, check)
	}
	s.say("  FAIL   unknown shared kind '%s'", kind)
	return errFailed
}

func (s *Setup) sharedRecord() string { return filepath.Join(s.Root, s.KitDir, SharedFileName) }

// InstallShared applies a shared file now and records it for sync and doctor. A failed apply is reported
// on Out and not returned (the record is still written), like the shell kit.
func (s *Setup) InstallShared(kind, dest, src string) error {
	_ = s.ApplyShared(kind, dest, src, false)
	line := kind + "\t" + dest + "\t" + src
	b, _ := os.ReadFile(s.sharedRecord())
	for _, l := range splitLines(string(b)) {
		if l == line {
			return nil
		}
	}
	return store.AppendLine(s.sharedRecord(), line)
}

// RefreshShared re-applies every recorded entry. With check it writes nothing and reports each drifting
// file; the error is non-nil when something is out of date or failed.
func (s *Setup) RefreshShared(check bool) error {
	b, err := os.ReadFile(s.sharedRecord())
	if err != nil {
		return nil
	}
	var bad error
	for _, l := range splitLines(string(b)) {
		f := strings.Split(l, "\t")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		if err := s.ApplyShared(f[0], f[1], f[2], check); err != nil {
			if check {
				s.say("  drift   %s (kit part is out of date)", f[1])
			}
			bad = fmt.Errorf("%s: %w", f[1], err)
		}
	}
	return bad
}
