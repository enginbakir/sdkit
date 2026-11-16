// Package audit is the append-only, hash-chained audit trail (what scripts/audit.sh did): who/what crossed a boundary, metadata only.
// Each line is a JSON object {ts,type,prev,<fields...>,h}; h is the sha256 of the line without h, prev the h of the line before ("0000" first).
// The bytes hashed are jq's compact output, so chains written by 1.x verify unchanged.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wecii/sdkit/internal/redact"
	"github.com/wecii/sdkit/internal/store"
)

// MaxValue is the longest value a field may have: the trail holds metadata, not content.
const MaxValue = 120

// Path is the trail of a project directory.
func Path(projectDir string) string { return filepath.Join(projectDir, "audit.jsonl") }

type kv struct{ k, v string } // v is the JSON text of the value

// object is a JSON object that keeps key order, as jq does (a repeated key keeps its first place and its last value).
type object []kv

func (o *object) set(k, v string) {
	for i := range *o {
		if (*o)[i].k == k {
			(*o)[i].v = v
			return
		}
	}
	*o = append(*o, kv{k, v})
}

func (o object) get(k string) (string, bool) {
	for _, e := range o {
		if e.k == k {
			return e.v, true
		}
	}
	return "", false
}

// str is the string value of key k as jq -r prints it ("null" when absent or null).
func (o object) str(k string) string {
	v, ok := o.get(k)
	var s string
	if ok && json.Unmarshal([]byte(v), &s) == nil {
		return s
	}
	if !ok || v == "null" {
		return "null"
	}
	return v
}

// encode renders the object as jq -c does.
func (o object) encode(skip string) string {
	var b strings.Builder
	b.WriteByte('{')
	for _, e := range o {
		if e.k == skip {
			continue
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.WriteString(jqString(e.k) + ":" + e.v)
	}
	b.WriteByte('}')
	return b.String()
}

// jqString quotes s like jq: control characters and DEL as \u00XX (\b \f \n \r \t short), everything else as is.
func jqString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func parse(line string) (object, error) {
	d := json.NewDecoder(strings.NewReader(line))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var o object
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		v := string(raw)
		var s string
		if json.Unmarshal(raw, &s) == nil {
			v = jqString(s)
		}
		o.set(t.(string), v)
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	return o, nil
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// ValidType reports whether t matches [a-z.]+.
func ValidType(t string) bool { return t != "" && strings.Trim(t, "abcdefghijklmnopqrstuvwxyz.") == "" }

// Log appends one entry. fields are key=value pairs; each value is redacted and at most MaxValue characters.
func Log(path, typ string, fields []string) error {
	if !ValidType(typ) {
		return errors.New("type must match [a-z.]+")
	}
	var o object
	for _, f := range fields {
		k, v, _ := strings.Cut(f, "=")
		if f == k { // no "=": the bash took the whole word as key and value
			v = f
		}
		if k == "" || strings.Trim(k, "abcdefghijklmnopqrstuvwxyz_") != "" {
			return fmt.Errorf("bad key '%s'", k)
		}
		if utf8.RuneCountInString(v) > MaxValue {
			return fmt.Errorf("value of %s is longer than %d characters; the trail holds metadata, not content", k, MaxValue)
		}
		o.set(k, jqString(strings.TrimRight(redact.Text(v), "\n")))
	}
	unlock, err := store.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	prev := "0000"
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		s := strings.TrimSuffix(string(b), "\n")
		last := s[strings.LastIndex(s, "\n")+1:]
		prev = "" // jq prints nothing for a last line that is not JSON
		if p, err := parse(last); err == nil {
			prev = p.str("h")
		}
	}
	body := object{{"ts", jqString(time.Now().UTC().Format("2006-01-02T15:04:05Z"))}, {"type", jqString(typ)}, {"prev", jqString(prev)}}
	for _, e := range o {
		body.set(e.k, e.v)
	}
	line := body.encode("")
	body.set("h", jqString(hash(line)))
	return store.AppendLine(path, body.encode(""))
}

// Verify recomputes the chain and returns the number of entries; on a break it returns the 1-based line of the first one.
func Verify(path string) (n, broken int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	prev := "0000"
	if len(b) == 0 {
		return 0, 0, nil
	}
	for i, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		o, perr := parse(line)
		h := ""
		if perr == nil {
			h = o.str("h")
		}
		if perr != nil || o.str("prev") != prev || hash(o.encode("h")) != h {
			return i, i + 1, nil
		}
		prev = h
		n = i + 1
	}
	return n, 0, nil
}

// Show returns the last n entries, of one type when typ is not empty, re-rendered as jq -c does.
func Show(path, typ string, n int) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		o, err := parse(line)
		if err != nil {
			continue
		}
		if t, _ := o.get("type"); typ == "" || t == jqString(typ) {
			out = append(out, o.encode(""))
		}
	}
	if n >= 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
