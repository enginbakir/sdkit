// Package secretscan finds credential-shaped strings in a repository: the working tree, the staged change and the git history.
// It is deterministic (RE2 rules, no network, no entropy guesses) and never keeps or prints a secret: a finding carries a masked excerpt and a hash.
// The kit's publishability scan (internal/devscan) and `sdkit scan secrets` share the rule table in this file.
package secretscan

import (
	"regexp"
	"strings"
)

// Kind groups rules the way the reports do: a token has a recognisable shape of its own, an assignment is a quoted
// value behind a credential-looking name, a URL credential is user:password@host.
type Kind string

const (
	Token      Kind = "token"
	Assignment Kind = "assignment"
	URLCred    Kind = "url-credential"
)

// Rule is one detector. ID is stable (a baseline entry names it), so renaming a rule invalidates baselines: add a new ID instead.
type Rule struct {
	ID       string
	Kind     Kind
	Severity string // "high" or "medium"
	Keyword  string // lower-case literal that must occur in the line for the regex to run; "" = always run
	Re       *regexp.Regexp
}

func rule(id string, kind Kind, sev, keyword, re string) Rule {
	return Rule{ID: id, Kind: kind, Severity: sev, Keyword: keyword, Re: regexp.MustCompile(re)}
}

// Rules is the table, in report order. Patterns are RE2: linear time, no backreferences.
var Rules = []Rule{
	rule("private-key", Token, "high", "private key", `-----BEGIN [A-Z ]*PRIVATE KEY`),
	rule("aws-key-id", Token, "high", "akia", `AKIA[0-9A-Z]{16}`),
	rule("github-token", Token, "high", "", `gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`),
	rule("slack-token", Token, "high", "xox", `xox[baprs]-[A-Za-z0-9-]{10,}`),
	rule("anthropic-key", Token, "high", "sk-ant-", `sk-ant-[A-Za-z0-9_-]{10,}`),
	rule("api-key", Token, "high", "sk-", `sk-[A-Za-z0-9]{32,}`),
	rule("google-api-key", Token, "high", "aiza", `AIza[0-9A-Za-z_-]{35}`),
	rule("bitbucket-token", Token, "high", "atbb", `ATBB[A-Za-z0-9_=-]{20,}`),
	rule("gitlab-token", Token, "high", "glpat-", `glpat-[A-Za-z0-9_-]{20,}`),
	rule("jwt", Token, "high", "eyj", `eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.`),
	rule("openai-key", Token, "high", "sk-", `sk-(proj|svcacct)-[A-Za-z0-9_-]{20,}`),
	rule("google-oauth-token", Token, "high", "ya29.", `ya29\.[A-Za-z0-9_-]{50,}`),
	rule("stripe-key", Token, "high", "_live_", `[rs]k_live_[0-9A-Za-z]{24,}`),
	rule("sendgrid-key", Token, "high", "sg.", `SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`),
	rule("twilio-key", Token, "high", "sk", `\bSK[0-9a-f]{32}\b`),
	rule("npm-token", Token, "high", "npm_", `npm_[A-Za-z0-9]{36}`),
	rule("pypi-token", Token, "high", "pypi-", `pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{50,}`),
	rule("docker-token", Token, "high", "dckr_pat_", `dckr_pat_[A-Za-z0-9_-]{27,}`),
	rule("huggingface-token", Token, "high", "hf_", `\bhf_[A-Za-z0-9]{34}\b`),
	rule("azure-storage-key", Token, "high", "accountkey=", `AccountKey=[A-Za-z0-9+/]{86}==`),
	rule("aws-secret-key", Assignment, "high", "secret", `(?i)aws_?secret_?access_?key[[:space:]]*[:=][[:space:]]*["']?[A-Za-z0-9/+=]{40}`),
	rule("connection-string-password", Assignment, "medium", "=", `(?i)\b(password|pwd)=[^;"'[:space:]$<{]{6,}(;|$)`),
	rule("quoted-credential", Assignment, "high", "", `(password|passwd|secret|api[_-]?key|apikey|access[_-]?token|auth[_-]?token|bearer)[A-Za-z_]*[[:space:]]*[:=][[:space:]]*["'][^"'$ ]{8,}["']`),
	rule("url-credential", URLCred, "high", "://", `://[^/[:space:]:@]+:[^/[:space:]@]+@[A-Za-z0-9.-]+`),
}

// Match reports whether the rule fires on line. The keyword is compared against the lower-cased line (pass it in, so one line is lowered once).
func (r Rule) Match(line, lower string) bool {
	if r.Keyword != "" && !strings.Contains(lower, r.Keyword) {
		return false
	}
	return r.Re.MatchString(line)
}

// Of returns the rules of one kind that fire on line, in table order.
func Of(kind Kind, line string) (hit []Rule) {
	lower := strings.ToLower(line)
	for _, r := range Rules {
		if r.Kind == kind && r.Match(line, lower) {
			hit = append(hit, r)
		}
	}
	return
}

// Mask keeps the first 4 characters of every match of a hard rule and hides the rest, so a report never prints a usable secret.
func Mask(line string) string {
	lower := strings.ToLower(line)
	for _, r := range Rules {
		if !r.Match(line, lower) {
			continue
		}
		line = r.Re.ReplaceAllStringFunc(line, func(m string) string {
			if len(m) <= 4 {
				return "***"
			}
			return m[:4] + "***"
		})
		lower = strings.ToLower(line)
	}
	return line
}
