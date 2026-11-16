// Package usage is the token ledger: one JSON line per agent run and model in <project-dir>/work/<slug>/usage.jsonl (schema v2, as in 1.x),
// the report that prices it from <project-dir>/pricing.json, the task budgets and the Claude Code transcript readers.
// Honesty rule: a missing record is unavailable or N/A, never a guess and never a 0 that reads as usage; no prompt text is ever stored.
package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/store"
)

// Num reads a JSON number leniently: null, a float or a missing value is an int (0 when unreadable).
type Num int

func (n *Num) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = Num(f)
	}
	return nil
}

// Line is one ledger line. TQ is "actual" (also when absent) or "unavailable": unmeasured runs keep zero counters only because nothing was read.
type Line struct {
	V          int      `json:"v"`
	TS         string   `json:"ts"`
	Role       string   `json:"role"`
	Tier       string   `json:"tier"`
	Host       string   `json:"host"`
	Model      string   `json:"model"`
	Provider   string   `json:"provider"`
	Session    string   `json:"session"`
	ID         string   `json:"id"`
	Origin     string   `json:"origin"`
	Kind       string   `json:"kind"`
	Source     string   `json:"source"`
	In         Num      `json:"in"`
	Out        Num      `json:"out"`
	CacheRead  Num      `json:"cache_read"`
	CacheWrite Num      `json:"cache_write"`
	TQ         string   `json:"tq,omitempty"`
	Status     string   `json:"status,omitempty"`
	CostEst    *float64 `json:"cost_est_usd,omitempty"` // the host's own client-side estimate, never a billed amount
	CostSrc    string   `json:"cost_src,omitempty"`
}

// Total is every counter of the line.
func (l Line) Total() int { return int(l.In + l.Out + l.CacheRead + l.CacheWrite) }

func (l Line) tq() string { return orStr(l.TQ, "actual") }

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func parseLine(raw string) (Line, bool) {
	var l Line
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &l) != nil {
		return l, false
	}
	return l, true
}

// Now is the ledger timestamp format.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func maxNum(a, b Num) Num {
	if b > a {
		return b
	}
	return a
}

// Log adds one line under the lock. The same (id, model) replaces an earlier line only when that loses nothing: a snapshot never replaces
// a final line or a larger reading, an unmeasured mark never replaces a measurement, and whatever replaces a reading keeps the larger value
// of each counter (counters only grow). An unmeasured placeholder without a model is not added once the execution has a measured line.
func Log(file string, n Line) error {
	unlock, err := store.Lock(file)
	if err != nil {
		return err
	}
	defer unlock()
	b, _ := os.ReadFile(file)
	var keep []string
	dropped := false
	if n.ID != "" && len(b) > 0 {
		all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		var ex *Line
		measured := false
		for _, raw := range all {
			if o, ok := parseLine(raw); ok && o.ID == n.ID {
				measured = measured || o.tq() != "unavailable"
				if o.Model == n.Model {
					o := o
					ex = &o
				}
			}
		}
		ntq := n.tq()
		if ex != nil && ex.tq() != "unavailable" {
			ek, nk := orStr(ex.Kind, "snapshot"), n.Kind
			if ntq == "unavailable" || (nk != "final" && (ek == "final" || n.Total() < ex.Total())) {
				return nil
			}
			n.In, n.Out = maxNum(n.In, ex.In), maxNum(n.Out, ex.Out)
			n.CacheRead, n.CacheWrite = maxNum(n.CacheRead, ex.CacheRead), maxNum(n.CacheWrite, ex.CacheWrite)
		}
		if ntq == "unavailable" && n.Model == "" && measured {
			return nil
		}
		for _, raw := range all {
			o, ok := parseLine(raw)
			if ok && o.ID == n.ID && (o.Model == n.Model || (ntq != "unavailable" && o.Model == "" && o.tq() == "unavailable")) {
				dropped = true
				continue
			}
			keep = append(keep, raw)
		}
	}
	var e bytes.Buffer
	enc := json.NewEncoder(&e)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(n)
	if !dropped {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write(e.Bytes())
		if e := f.Close(); err == nil {
			err = e
		}
		return err
	}
	return store.WriteAtomic(file, []byte(strings.Join(keep, "\n")+"\n"+e.String()))
}

// ReadLines is every object line of a ledger file (a line that is not an object is skipped).
func ReadLines(file string) []Line {
	b, _ := os.ReadFile(file)
	var out []Line
	for _, raw := range strings.Split(string(b), "\n") {
		if l, ok := parseLine(raw); ok {
			out = append(out, l)
		}
	}
	return out
}
