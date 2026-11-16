// Package glob implements shell `case` pattern matching for the guards.
// Unlike path.Match, `*` and `?` also match "/".
package glob

import "strings"

// Match reports whether s matches the shell pattern: * ? [abc] [a-z] [!abc]
// [^abc] and backslash escape. An unterminated "[" is a literal.
func Match(pattern, s string) bool { return match([]rune(pattern), []rune(s)) }

func match(p, s []rune) bool {
	star := -1 // index in p after the last *
	mark := 0  // s position to retry from
	i, j := 0, 0
	for j < len(s) {
		if i < len(p) {
			switch p[i] {
			case '*':
				for i < len(p) && p[i] == '*' {
					i++
				}
				star, mark = i, j
				continue
			case '?':
				i++
				j++
				continue
			case '[':
				if ok, n, valid := class(p[i:], s[j]); valid {
					if ok {
						i += n
						j++
						continue
					}
					goto retry
				}
			case '\\':
				if i+1 < len(p) {
					if p[i+1] == s[j] {
						i += 2
						j++
						continue
					}
					goto retry
				}
			}
			if p[i] == s[j] {
				i++
				j++
				continue
			}
		}
	retry:
		if star < 0 {
			return false
		}
		mark++
		i, j = star, mark
	}
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}

// class matches c against the bracket expression at the start of p. It
// returns the match result, the expression length, and whether the
// expression is terminated.
func class(p []rune, c rune) (ok bool, n int, valid bool) {
	i := 1
	neg := false
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		neg = true
		i++
	}
	first := true
	for i < len(p) {
		if p[i] == ']' && !first {
			return ok != neg, i + 1, true
		}
		first = false
		lo := p[i]
		if lo == '\\' && i+1 < len(p) {
			i++
			lo = p[i]
		}
		hi := lo
		if i+2 < len(p) && p[i+1] == '-' && p[i+2] != ']' {
			hi = p[i+2]
			i += 2
		}
		if lo <= c && c <= hi {
			ok = true
		}
		i++
	}
	return false, 0, false
}

func fold(fold bool, s string) string {
	if fold {
		return strings.ToLower(s)
	}
	return s
}

func base(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func split(list string) []string {
	parts := strings.Split(list, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchList reports whether the basename or the whole path of path matches
// any comma-separated glob. Empty entries are skipped. foldCase compares
// case-insensitively (Windows).
func MatchList(path, globs string, foldCase bool) bool {
	path = fold(foldCase, path)
	b := base(path)
	for _, g := range split(fold(foldCase, globs)) {
		if Match(g, b) || Match(g, path) {
			return true
		}
	}
	return false
}

// HasPrefix reports whether path starts with any comma-separated prefix,
// as a literal string prefix (scripts/guard.sh: case "$f" in "$p"*).
func HasPrefix(path, prefixes string, foldCase bool) bool {
	path = fold(foldCase, path)
	for _, p := range split(fold(foldCase, prefixes)) {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// HasDirPrefix is the hook variant (hooks/guard-protected.sh: "$prefix"/*):
// path must start with prefix followed by "/".
func HasDirPrefix(path, prefixes string, foldCase bool) bool {
	path = fold(foldCase, path)
	for _, p := range split(fold(foldCase, prefixes)) {
		if strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
