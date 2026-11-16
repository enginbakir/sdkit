// Package plan checks the plan documents: the Definition of Done, plan/proposal/tasks
// parity, task write sets and parallel waves.
package plan

import (
	"fmt"
	"regexp"
	"strings"
)

var reItem = regexp.MustCompilePOSIX(`^[ \t]*([-*][ \t]+)?[*_]*D([0-9]+)[*_]*[.:)]`)

// reVerified finds "verified by", an optional colon and the decoration after it.
var reHeading = regexp.MustCompile(`^#+[ \t]`)

var reVerified = regexp.MustCompilePOSIX(`verified by:?[ \t*_]*`)

// lower lowercases ASCII only, so byte offsets stay valid.
func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// section returns the lines under the first heading containing want (case-insensitive),
// or the whole text when there is none (or it is empty).
func section(text, want string) string {
	var out []string
	inside := false
	for _, l := range strings.Split(text, "\n") {
		if reHeading.MatchString(l) {
			if inside {
				break
			}
			if strings.Contains(lower(l), want) {
				inside = true
			}
			continue
		}
		if inside {
			out = append(out, l)
		}
	}
	if s := strings.TrimRight(strings.Join(out, "\n"), "\n"); s != "" {
		return s
	}
	return text
}

func trimDecor(s string) string {
	s = strings.TrimLeft(s, " \t*_`")
	return strings.TrimRight(s, " \t*_`.;,")
}

// splitItem parses a "D<n>: text | Verified by: method" line.
func splitItem(line string) (id, text, vb string, ok bool) {
	m := reItem.FindStringSubmatchIndex(line)
	if m == nil {
		return "", "", "", false
	}
	id = "D" + line[m[4]:m[5]]
	rest := line[m[1]:]
	if loc := reVerified.FindStringIndex(lower(rest)); loc != nil {
		vb = trimDecor(rest[loc[1]:])
		text = strings.TrimSuffix(trimDecor(rest[:loc[0]]), "|")
		return id, trimDecor(text), vb, true
	}
	return id, trimDecor(rest), "", true
}

var (
	reNone   = regexp.MustCompile(`^none[ \t]*[:\-—–][ \t]*`)
	rePost   = regexp.MustCompile(`post-?deploy|canary|production smoke|after deploy|post-?release`)
	reStatic = regexp.MustCompile(`lint|typecheck|type-check|static|inspection|review|schema check|grep`)
	reAuto   = regexp.MustCompile(`test|e2e|automated|contract|regression|benchmark|assert`)
)

// CheckDoD validates the DoD items of a proposal or plan. out holds the lines to print;
// ok is false when there are problems (exit 1).
func CheckDoD(text string) (out []string, ok bool) {
	seen := map[string]bool{}
	c := map[string]int{}
	n, bad := 0, 0
	for _, l := range strings.Split(section(text, "definition of done"), "\n") {
		id, body, vb, isItem := splitItem(l)
		if !isItem {
			continue
		}
		if seen[id] {
			out = append(out, "duplicate id "+id)
			bad++
		}
		seen[id] = true
		if body == "" {
			out = append(out, id+": empty condition")
			bad++
		}
		if vb == "" {
			out = append(out, id+": no \"Verified by:\" method")
			bad++
			continue
		}
		lv, cls := lower(vb), ""
		switch {
		case strings.HasPrefix(lv, "none"):
			if r := reNone.ReplaceAllString(lv, ""); r == "" || r == lv {
				out = append(out, id+": \"none\" needs a reason (none: <reason>)")
				bad++
			} else {
				cls = "none"
			}
		case rePost.MatchString(lv):
			cls = "post-deploy"
		case strings.Contains(lv, "manual"):
			cls = "manual"
		case reStatic.MatchString(lv):
			cls = "static"
		case reAuto.MatchString(lv):
			cls = "automated"
		default:
			out = append(out, "warn: "+id+": unclassified method \""+vb+"\", counted as manual (it needs recorded evidence; prefer automated|static|manual|post-deploy|none: reason)")
			cls = "manual"
		}
		if cls != "" {
			c[cls]++
		}
		n++
	}
	if n == 0 {
		return append(out, "no DoD items (D<n>: ... | Verified by: ...) found"), false
	}
	if bad > 0 {
		return out, false
	}
	out = append(out, fmt.Sprintf("dod ok: %d item(s) automated=%d static=%d manual=%d post-deploy=%d none=%d", n, c["automated"], c["static"], c["manual"], c["post-deploy"], c["none"]))
	if c["manual"]+c["post-deploy"]+c["none"] > 0 {
		out = append(out, "note: manual / post-deploy / none items are not satisfied by a verifier PASS without recorded evidence; plan how each is evidenced or accepted.")
	}
	return out, true
}
