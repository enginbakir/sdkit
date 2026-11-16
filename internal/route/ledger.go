package route

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/store"
	"github.com/wecii/sdkit/internal/usage"
)

// Classify is the failure class of an exit status (a routing outcome 7 or 8 is not a host failure). rc is the process status, prc the
// verdict status (1 = BLOCK), stderr the tail of the host's error output.
func Classify(rc, prc int, stderr string) string {
	switch rc {
	case 0:
		if prc == 1 {
			return "VERDICT_BLOCK"
		}
		return "OK"
	case 3:
		return "BUDGET_EXHAUSTED"
	case 4:
		return "CONTRACT_FAILURE"
	case 5:
		return "REPORTED_FAILURE"
	case 6:
		return "INVALID_OUTPUT"
	case 130, 143:
		return "INTERRUPTED"
	case 127:
		return "HOST_MISSING"
	case 124:
		return "TIMEOUT"
	case 7:
		return "NOT_RUN"
	}
	l := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	t := strings.ToLower(strings.Join(l[max(len(l)-40, 0):], "\n"))
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(t, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("rate limit", "rate_limit", "429", "too many requests", "quota", "overloaded"):
		return "RATE_LIMIT"
	case has("timed out", "timeout", "etimedout", "deadline exceeded"):
		return "TIMEOUT"
	case has("econnreset", "connection reset", "temporarily unavailable", "service unavailable", "502", "503", "504", "network", "econnrefused"):
		return "TRANSIENT"
	}
	return "UNKNOWN"
}

func (r *Router) work() string { return filepath.Join(r.P.Dir(), "work") }

func (r *Router) ledger(slug string) string { return filepath.Join(r.work(), slug, "routing.jsonl") }

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func readJSONL(path string) (out []map[string]any, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *Router) readEntries(slug string) (all []Entry) {
	f, err := os.Open(r.ledger(slug))
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Type != "" {
			all = append(all, e)
		}
	}
	return all
}

func (r *Router) appendJSON(path string, v any) error {
	b, err := enc(v)
	if err != nil {
		return err
	}
	return store.AppendLine(path, string(b))
}

// Reservation is money held for a run that has not reported an outcome yet.
type Reservation struct {
	Slug  string  `json:"slug"`
	ID    string  `json:"id"`
	USD   float64 `json:"usd"`
	AgeS  int     `json:"age_s"`
	PID   *int    `json:"pid"`
	State string  `json:"state"` // active | stale
}

// Reservations lists every reservation without a release, over all tasks. Stale means older than ROUTE_RESERVE_TTL_S AND its process
// is gone: a live process keeps its reservation however long it runs. An unreadable ledger is an error, nothing is guessed.
func (r *Router) Reservations() ([]Reservation, error) {
	ttl := 3600
	if v, err := strconv.Atoi(r.Get("ROUTE_RESERVE_TTL_S")); err == nil {
		ttl = v
	}
	files, _ := filepath.Glob(filepath.Join(r.work(), "*", "reservations.jsonl"))
	var out []Reservation
	for _, f := range files {
		lines, err := readJSONL(f)
		if err != nil {
			return nil, fmt.Errorf("%s cannot be read", f)
		}
		done := map[string]bool{}
		for _, m := range lines {
			if m["type"] == "release" {
				id, _ := m["id"].(string)
				done[id] = true
			}
		}
		for _, m := range lines {
			id, _ := m["id"].(string)
			if m["type"] != "reserve" || done[id] {
				continue
			}
			usd, ok := m["usd"].(float64)
			if !ok {
				return nil, fmt.Errorf("%s: a reservation has no amount", f)
			}
			ts, _ := m["ts"].(string)
			t, err := time.Parse("2006-01-02T15:04:05Z", ts)
			if err != nil {
				t = time.Unix(0, 0)
			}
			x := Reservation{Slug: filepath.Base(filepath.Dir(f)), ID: id, USD: usd, AgeS: int(r.now().Sub(t) / time.Second), State: "active"}
			if p, ok := m["pid"].(float64); ok {
				pid := int(p)
				x.PID = &pid
			}
			if x.AgeS > ttl && !(x.PID != nil && *x.PID > 0 && proc.Alive(*x.PID)) {
				x.State = "stale"
			}
			out = append(out, x)
		}
	}
	return out, nil
}

// OpenReserved is the USD held by runs that have not reported yet and are not stale.
func (r *Router) OpenReserved() (float64, error) {
	l, err := r.Reservations()
	t := 0.0
	for _, x := range l {
		if x.State == "active" {
			t += x.USD
		}
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(t, 'f', 6, 64), 64)
	return v, err
}

// Release gives a reservation back (idempotent: a second call, or a late outcome, appends nothing).
func (r *Router) Release(slug, id, class, note string) {
	f := filepath.Join(r.work(), slug, "reservations.jsonl")
	if _, err := os.Stat(f); err != nil {
		return
	}
	if u, err := store.Lock(filepath.Join(r.work(), ".project-budget")); err == nil {
		defer u()
	}
	lines, _ := readJSONL(f)
	usd := 0.0
	for _, m := range lines {
		if m["id"] == id && m["type"] == "release" {
			return
		}
		if m["id"] == id && m["type"] == "reserve" && usd == 0 {
			usd, _ = m["usd"].(float64)
		}
	}
	if note == "" {
		note = "actual spend is in usage.jsonl; release returns the reservation"
	}
	_ = r.appendJSON(f, struct {
		Type string  `json:"type"`
		ID   string  `json:"id"`
		TS   string  `json:"ts"`
		USD  float64 `json:"reserved_usd"`
		C    string  `json:"class"`
		Note string  `json:"note"`
	}{"release", id, stamp(r.now()), usd, class, note})
}

// Expire releases every stale reservation and returns how many.
func (r *Router) Expire() (int, error) {
	l, err := r.Reservations()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range l {
		if x.State == "stale" {
			pid := ""
			if x.PID != nil {
				pid = strconv.Itoa(*x.PID) + " "
			}
			r.Release(x.Slug, x.ID, "EXPIRED", fmt.Sprintf("expired after %ds (older than ROUTE_RESERVE_TTL_S and its process %sis gone); released by sdkit route reservations expire", x.AgeS, pid))
			n++
		}
	}
	return n, nil
}

// cost is the USD of one ledger line from the user's price table; ok is false when the model has no price or a counter has no rate.
func cost(pt *pricing.Table, l usage.Line) (float64, bool) {
	rt, ok := pt.Lookup(l.Model)
	if !ok || l.CacheRead > 0 && rt.CacheRead == 0 || l.CacheWrite > 0 && rt.CacheWrite == 0 {
		return 0, false
	}
	return (float64(l.In)*rt.In + float64(l.Out)*rt.Out + float64(l.CacheRead)*rt.CacheRead + float64(l.CacheWrite)*rt.CacheWrite) / 1e6, true
}

func baseRole(s string) string { return strings.TrimSuffix(s, "-deep") }

// usageFacts reads the task's usage ledger.
func usageFacts(lines []usage.Line, pt *pricing.Table, base string) (u Usage) {
	var est, hc float64
	priced := 0
	for _, l := range lines {
		u.N++
		un := l.TQ == "unavailable"
		if un {
			u.Unavailable++
		} else {
			u.Tokens += int(l.In + l.Out + l.CacheWrite)
			if baseRole(l.Role) == base {
				u.RoleTokens += int(l.In + l.Out + l.CacheWrite)
			}
		}
		if l.CostEst != nil {
			hc += *l.CostEst
			u.HostRuns++
		}
		if c, ok := cost(pt, l); !un && ok {
			est += c
			priced++
		}
	}
	u.Unpriced = u.N - priced
	if priced > 0 {
		u.Est = &est
	}
	if u.HostRuns > 0 {
		u.HostEst = &hc
	}
	return u
}

// spend is the priced USD of the lines (a role filter when role is set) and the runs that are unmeasured or unpriced.
func spend(files []string, pt *pricing.Table, role string) (s Spend) {
	for _, f := range files {
		for _, l := range usage.ReadLines(f) {
			if role != "" && baseRole(l.Role) != role {
				continue
			}
			if c, ok := cost(pt, l); l.TQ != "unavailable" && ok {
				s.USD += c
			} else {
				s.Unknown++
			}
		}
	}
	return s
}
