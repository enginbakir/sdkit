// Package redact replaces credential-shaped strings with [REDACTED:<kind>] (what scripts/redact.sh did). Deterministic patterns only:
// a filter for views and logs, not a guarantee that no secret remains.
package redact

import (
	"regexp"
	"strings"
)

type rule struct {
	re  *regexp.Regexp
	tag string
}

func rl(p, tag string) rule {
	re := regexp.MustCompile(p)
	re.Longest() // awk's ERE matches leftmost-longest
	return rule{re, tag}
}

var (
	rules = []rule{
		rl(`AKIA[0-9A-Z]{16}`, "[REDACTED:aws-key-id]"),
		rl(`(ghp|gho|ghs|ghu)_[A-Za-z0-9]{20,}`, "[REDACTED:github-token]"),
		rl(`github_pat_[A-Za-z0-9_]{20,}`, "[REDACTED:github-token]"),
		rl(`xox[abprs]-[A-Za-z0-9-]{10,}`, "[REDACTED:slack-token]"),
		rl(`sk-(ant-)?[A-Za-z0-9_-]{16,}`, "[REDACTED:api-key]"),
		rl(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`, "[REDACTED:jwt]"),
	}
	bearer   = rl(`[Bb]earer[ \t]+[A-Za-z0-9._~+/=-]{12,}`, "").re
	bearerWS = regexp.MustCompile(`[ \t]+[^ \t]+$`)
	assign   = rl(`(password|passwd|secret|token|api[_-]?key)[ \t]*[:=][ \t]*["']?[^ \t"']{6,}`, "").re
	valueTop = regexp.MustCompile(`[^ \t:="']+$`)
	keyBegin = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	keyEnd   = regexp.MustCompile(`-----END [A-Z ]*PRIVATE KEY-----`)
)

// Patterns lists the pattern sources, for the policy check that each one is RE2.
func Patterns() []string {
	return []string{bearer.String(), assign.String(), keyBegin.String(), keyEnd.String()}
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Line redacts one line (no private-key block handling).
func Line(l string) string {
	for _, r := range rules {
		l = r.re.ReplaceAllLiteralString(l, r.tag)
	}
	l = bearer.ReplaceAllStringFunc(l, func(hit string) string { return bearerWS.ReplaceAllLiteralString(hit, " [REDACTED:bearer]") })
	var out strings.Builder
	for {
		m := assign.FindStringIndex(lowerASCII(l))
		if m == nil {
			break
		}
		out.WriteString(l[:m[0]])
		out.WriteString(valueTop.ReplaceAllLiteralString(l[m[0]:m[1]], ""))
		out.WriteString("[REDACTED:value]")
		l = l[m[1]:]
	}
	return out.String() + l
}

// Text redacts text line by line; a private-key block becomes one [REDACTED:private-key] line (an unterminated one swallows the rest).
// Every output line ends with a newline, an empty input gives an empty output, as the awk filter did.
func Text(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	in := false
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if keyBegin.MatchString(l) {
			in = true
		}
		if in {
			if keyEnd.MatchString(l) {
				in = false
				out.WriteString("[REDACTED:private-key]\n")
			}
			continue
		}
		out.WriteString(Line(l) + "\n")
	}
	return out.String()
}
