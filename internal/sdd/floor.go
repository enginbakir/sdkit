package sdd

import (
	"fmt"
	"strings"

	"github.com/wecii/sdkit/internal/glob"
)

// RISK_FLOOR_GLOBS=level:glob[,level:glob...]  level = low|medium|high, glob = the kind PROTECTED_PATHS/SECRET_GLOBS use.
// A task whose diff touches a matching file may not carry a lower risk label than the strictest matching level. It is
// checked when the tests go green, when the task is verified, and in ci-check. The label stays the user's; the floor
// only stops it from being too low for what the diff really touches.

// FloorValidate returns why globs is malformed, "" when it is empty or well-formed.
func FloorValidate(globs string) string {
	if globs == "" {
		return ""
	}
	for _, entry := range strings.Split(globs, ",") {
		if entry == "" {
			continue
		}
		lvl, g, ok := strings.Cut(entry, ":")
		if !ok || g == "" || (lvl != "low" && lvl != "medium" && lvl != "high") {
			return "'" + entry + "' is not <low|medium|high>:<glob>"
		}
	}
	return ""
}

// FloorOf is the strictest level a file of files sets and the first file that sets it ("low" and "" when none does).
func FloorOf(globs string, files []string) (level, file string) {
	level = "low"
	for _, f := range files {
		for _, entry := range strings.Split(globs, ",") {
			lvl, g, _ := strings.Cut(entry, ":")
			if entry == "" || !glob.MatchList(f, g, false) || rank(lvl) <= rank(level) {
				continue
			}
			level, file = lvl, f
		}
	}
	return level, file
}

// floorHit: does any entry match the file, at any level?
func floorHit(globs, f string) bool {
	for _, entry := range strings.Split(globs, ",") {
		if _, g, _ := strings.Cut(entry, ":"); entry != "" && glob.MatchList(f, g, false) {
			return true
		}
	}
	return false
}

// Floor prints the floor of the diff: "<level> <file>", or "low". Exit 2 and the reason when the setting is invalid.
func (e *Env) Floor(validate bool) int {
	globs := e.Get("RISK_FLOOR_GLOBS")
	if why := FloorValidate(globs); why != "" {
		e.printf("%s\n", why)
		return 2
	}
	if validate {
		return 0
	}
	if globs == "" {
		e.printf("low\n")
		return 0
	}
	files, _ := e.ChangedFiles() // a diff that cannot be listed has no floor here; the fingerprint fails closed
	lvl, f := FloorOf(globs, files)
	e.printf("%s\n", strings.TrimSpace(lvl+" "+f))
	return 0
}

// enforceFloor: strict refuses a task labelled below the floor of its diff, guided warns, off does nothing.
func (e *Env) enforceFloor(risk string) *Refusal {
	globs, mode := e.Get("RISK_FLOOR_GLOBS"), e.Mode()
	if globs == "" || mode == "off" {
		return nil
	}
	var msg string
	if why := FloorValidate(globs); why != "" {
		msg = "RISK_FLOOR_GLOBS is invalid: " + why + " (expected level:glob,level:glob with level low|medium|high)"
	} else {
		files, _ := e.ChangedFiles()
		lvl, f := FloorOf(globs, files)
		if rank(risk) >= rank(lvl) {
			return nil
		}
		msg = fmt.Sprintf("the diff touches '%s', which needs risk %s or higher (RISK_FLOOR_GLOBS), but this task is '%s'. A label cannot be lowered to fit a diff: ask the user to confirm the higher label, run 'sdkit sdd abort', and open the task again with --risk %s.", f, lvl, risk, lvl)
	}
	if mode == "guided" {
		e.warnf("sdd (warning): %s\n", msg)
		return nil
	}
	return &Refusal{"risk-floor", msg}
}
