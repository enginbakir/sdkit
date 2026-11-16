package plan

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotAcceptable is returned for an item whose method needs evidence, not consent.
var ErrNotAcceptable = errors.New("not acceptable")

var reLastVerified = regexp.MustCompile(`.*verified by:?[ *_]*`)

// Item returns the whitespace-normalised first line of text that defines DoD item id ("" if none).
func Item(text, id string) string {
	re := regexp.MustCompile(`^[ \t\v\f\r]*([-*][ \t\v\f\r]+)?[*_]*` + regexp.QuoteMeta(id) + `[*_]*[.:)]`)
	for _, l := range strings.Split(text, "\n") {
		if re.MatchString(l) {
			return strings.Join(strings.Fields(l), " ")
		}
	}
	return ""
}

func itemHash(line string) string { h := sha256.Sum256([]byte(line)); return hex.EncodeToString(h[:]) }

type record struct {
	V        int    `json:"v"`
	TS       string `json:"ts"`
	ID       string `json:"id"`
	Item     string `json:"item"`
	SHA      string `json:"item_sha256"`
	By       string `json:"by"`
	Reason   string `json:"reason"`
	Proposal string `json:"proposal"`
}

// Accepted reports whether acceptances file af holds an acceptance of id for the item exactly as line reads now.
func Accepted(af, id, line string) bool {
	f, err := os.Open(af)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID == id && r.SHA == itemHash(line) {
			return true
		}
	}
	return false
}

// Accept appends an acceptance of item line (id) of proposal file pf to acceptances.jsonl next to it.
// Only manual, post-deploy and none items can be accepted.
func Accept(pf, id, line, by, reason string) (af string, err error) {
	l := lower(line)
	cls := ""
	if m := reLastVerified.FindString(l); m != "" {
		cls = l[len(m):]
	}
	if !(strings.HasPrefix(cls, "manual") || strings.HasPrefix(cls, "none") || strings.Contains(cls, " manual") || rePost.MatchString(cls)) {
		return "", fmt.Errorf("%w: %s is verified by '%s': only manual, post-deploy or none items can be accepted (the others need evidence)", ErrNotAcceptable, id, cls)
	}
	af = filepath.Dir(pf) + "/acceptances.jsonl"
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(record{1, time.Now().UTC().Format("2006-01-02T15:04:05Z"), id, line, itemHash(line), by, reason,
		filepath.Base(filepath.Dir(pf)) + "/" + filepath.Base(pf)})
	f, err := os.OpenFile(af, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return af, err
	}
	defer f.Close()
	_, err = f.Write(b.Bytes())
	return af, err
}
