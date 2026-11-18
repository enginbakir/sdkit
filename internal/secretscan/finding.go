package secretscan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/wecii/sdkit/internal/redact"
)

// Where a finding was seen.
const (
	SourceTree    = "tree"
	SourceStaged  = "staged"
	SourceHistory = "history"
)

// Finding is one credential-shaped string. It never holds the secret: Excerpt is masked and Fingerprint is a truncated hash.
// Field order is the JSON order (the file is compared and may be committed, so it must be deterministic).
type Finding struct {
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Line        int    `json:"line,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Excerpt     string `json:"excerpt"`
	Fingerprint string `json:"fingerprint"`
}

// Fingerprint identifies a finding across line moves and commits: rule, path and the secret itself, not the line number.
// 24 hex characters keep a committed baseline from being a usable hash of a short secret.
func Fingerprint(rule, path, secret string) string {
	s := sha256.Sum256([]byte(rule + "\x00" + path + "\x00" + secret))
	return hex.EncodeToString(s[:])[:24]
}

// excerpt is the line with every secret hidden, trimmed for a report.
func excerpt(line string) string {
	l := strings.TrimSpace(redact.Line(Mask(line)))
	if len(l) > 120 {
		l = l[:120]
	}
	return l
}

// Report is the content of work/scan/findings.json. Baselined findings are listed apart: accepting one never hides it.
type Report struct {
	Version   int       `json:"version"`
	Findings  []Finding `json:"findings"`
	Baselined []Finding `json:"baselined"`
}

// JSON renders the report; empty lists are [] not null.
func (r Report) JSON() []byte {
	r.Version = 1
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	if r.Baselined == nil {
		r.Baselined = []Finding{}
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return append(b, '\n')
}
