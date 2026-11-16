package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// obj is a JSON object that remembers key order: committed JSON keeps the
// order jq gave it (new keys last), so no map iteration is used for output.
type obj struct {
	keys []string
	m    map[string]any
}

func newObj() *obj { return &obj{m: map[string]any{}} }

func (o *obj) get(k string) (any, bool) { v, ok := o.m[k]; return v, ok }

// set replaces the value of k in place, or appends k.
func (o *obj) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

// parseJSON reads one JSON document; numbers keep their literal text.
func parseJSON(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := parseValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON value")
	}
	return v, nil
}

func parseValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	dl, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	if dl == '[' {
		a := []any{}
		for d.More() {
			v, err := parseValue(d)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	}
	o := newObj()
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return nil, err
		}
		v, err := parseValue(d)
		if err != nil {
			return nil, err
		}
		o.set(k.(string), v)
	}
	_, err = d.Token()
	return o, err
}

// marshal prints like `jq .` (2 spaces, raw UTF-8); sorted is `jq -S .`.
func marshal(v any, sorted bool) []byte {
	var b bytes.Buffer
	write(&b, v, "", sorted)
	b.WriteByte('\n')
	return b.Bytes()
}

func write(b *bytes.Buffer, v any, ind string, sorted bool) {
	switch x := v.(type) {
	case *obj:
		if len(x.keys) == 0 {
			b.WriteString("{}")
			return
		}
		keys := x.keys
		if sorted {
			keys = append([]string(nil), keys...)
			sort.Strings(keys)
		}
		b.WriteString("{\n")
		for i, k := range keys {
			b.WriteString(ind + "  ")
			quote(b, k)
			b.WriteString(": ")
			write(b, x.m[k], ind+"  ", sorted)
			if i < len(keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(ind + "}")
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range x {
			b.WriteString(ind + "  ")
			write(b, e, ind+"  ", sorted)
			if i < len(x)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(ind + "]")
	case string:
		quote(b, x)
	case json.Number:
		b.WriteString(x.String())
	case bool:
		fmt.Fprint(b, x)
	default:
		b.WriteString("null")
	}
}

func quote(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
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
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}

// canon is the comparison form of a value (sorted keys).
func canon(v any) string { return string(marshal(v, true)) }

func asObj(v any) (*obj, error) {
	switch x := v.(type) {
	case nil:
		return newObj(), nil
	case *obj:
		return x, nil
	}
	return nil, errors.New("expected a JSON object")
}

// addMissing is jq `.[key] = ({kit} + {user})`: kit keys first, user values win.
func addMissing(doc, kitDoc *obj, key string) error {
	c, err := asObj(doc.m[key])
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	k, err := asObj(kitDoc.m[key])
	if err != nil {
		return err
	}
	out := newObj()
	for _, n := range k.keys {
		out.set(n, k.m[n])
	}
	for _, n := range c.keys {
		out.set(n, c.m[n])
	}
	doc.set(key, out)
	return nil
}

// addList appends the kit's entries of doc[path...] that the user list lacks.
func addList(doc, kitDoc *obj, path ...string) error {
	parent, kparent := doc, kitDoc
	for _, p := range path[:len(path)-1] {
		child, err := asObj(parent.m[p])
		if err != nil {
			return err
		}
		parent.set(p, child)
		parent = child
		kparent, err = asObj(kparent.m[p])
		if err != nil {
			return err
		}
	}
	last := path[len(path)-1]
	cur, _ := parent.m[last].([]any)
	if parent.m[last] != nil && cur == nil {
		return errors.New(last + " is not a list")
	}
	add, _ := kparent.m[last].([]any)
	seen := map[string]bool{}
	for _, e := range cur {
		seen[canon(e)] = true
	}
	out := append([]any{}, cur...)
	for _, e := range add {
		if !seen[canon(e)] {
			out = append(out, e)
		}
	}
	parent.set(last, out)
	return nil
}

func owned(h any, re *regexp.Regexp) bool {
	o, _ := h.(*obj)
	if o == nil {
		return false
	}
	c, _ := o.m["command"].(string)
	if args, _ := o.m["args"].([]any); len(args) > 0 { // exec form: {"command": "sdkit", "args": ["hook", ...]}
		c += " " + fmt.Sprint(args[0])
	}
	return re.MatchString(c)
}

// mergeHooks drops the kit-owned entries of doc.hooks and appends the kit's per event.
// grouped is the Gemini/Claude shape {matcher, hooks:[{command}]}, else Cursor's flat entries.
func mergeHooks(doc, kitDoc *obj, re *regexp.Regexp, grouped bool) error {
	user, err := asObj(doc.m["hooks"])
	if err != nil {
		return err
	}
	kh, err := asObj(kitDoc.m["hooks"])
	if err != nil {
		return err
	}
	mine := newObj()
	for _, ev := range user.keys {
		list, ok := user.m[ev].([]any)
		if !ok {
			return errors.New("hooks." + ev + " is not a list")
		}
		keep := []any{}
		for _, e := range list {
			if !grouped {
				if !owned(e, re) {
					keep = append(keep, e)
				}
				continue
			}
			g, ok := e.(*obj)
			inner, isList := []any(nil), false
			if ok {
				inner, isList = g.m["hooks"].([]any)
			}
			if !isList {
				return errors.New("hooks." + ev + " entry has no hooks list")
			}
			rest := []any{}
			for _, h := range inner {
				if !owned(h, re) {
					rest = append(rest, h)
				}
			}
			if len(rest) > 0 {
				g.set("hooks", rest)
				keep = append(keep, g)
			}
		}
		mine.set(ev, keep)
	}
	evs := append([]string(nil), kh.keys...)
	sort.Strings(evs) // jq `keys` is sorted
	for _, ev := range evs {
		cur, _ := mine.m[ev].([]any)
		add, _ := kh.m[ev].([]any)
		mine.set(ev, append(append([]any{}, cur...), add...))
	}
	doc.set("hooks", mine)
	return nil
}

var (
	claudeOwned = regexp.MustCompile(`/hooks/[A-Za-z0-9_-]+\.sh|sdkit hook`)
	adaptOwned  = regexp.MustCompile(`adapt\.sh|sdkit hook`)
)

// mergeKind applies the kit document to the user's, touching only kit-owned keys and entries.
// kind: claude-settings, hooks-grouped (Gemini) or hooks-flat (Cursor).
func mergeKind(kind string, doc, kitDoc *obj) error {
	switch kind {
	case "claude-settings":
		for _, f := range []func() error{
			func() error { return addMissing(doc, kitDoc, "enabledPlugins") },
			func() error { return addMissing(doc, kitDoc, "extraKnownMarketplaces") },
			func() error { return addList(doc, kitDoc, "permissions", "allow") },
			func() error { return addList(doc, kitDoc, "permissions", "deny") },
			func() error {
				if kitDoc.m["hooks"] == nil {
					return nil
				}
				return mergeHooks(doc, kitDoc, claudeOwned, true)
			},
		} {
			if err := f(); err != nil {
				return err
			}
		}
	case "hooks-grouped":
		if err := mergeHooks(doc, kitDoc, adaptOwned, true); err != nil {
			return err
		}
		return addList(doc, kitDoc, "context", "fileName")
	default:
		if v, ok := doc.m["version"]; (!ok || v == nil) && kitDoc.m["version"] != nil {
			doc.set("version", kitDoc.m["version"])
		}
		return mergeHooks(doc, kitDoc, adaptOwned, false)
	}
	return nil
}

// addPermissions is jq `.permissions.allow += extra`.
func addPermissions(kitDoc *obj, extra []string) error {
	p, err := asObj(kitDoc.m["permissions"])
	if err != nil {
		return err
	}
	cur, _ := p.m["allow"].([]any)
	for _, e := range extra {
		cur = append(cur, e)
	}
	if cur == nil {
		cur = []any{}
	}
	p.set("allow", cur)
	kitDoc.set("permissions", p)
	return nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
