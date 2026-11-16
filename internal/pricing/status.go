package pricing

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status is the state of the table, what `sdkit pricing status` and the doctor warning block print.
type Status struct {
	File     string   `json:"file"`
	State    string   `json:"state"` // missing | unreadable | custom | managed
	AgeDays  *int     `json:"age_days"`
	Models   int      `json:"models"`
	Unpriced []string `json:"unpriced_models"`
	Problems []string `json:"problems"` // empty means fine
}

// StatusOptions: File is the table, WorkDir the project's work/ directory (ledgers are read from <WorkDir>/*/usage.jsonl and _archive/*.usage.jsonl).
type StatusOptions struct {
	File          string
	WorkDir       string
	BaselineModel string
	StaleDays     int // default 90
	Now           time.Time
}

// Check inspects the table; it never fails.
func Check(o StatusOptions) Status {
	if o.StaleDays == 0 {
		o.StaleDays = 90
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	st := Status{File: o.File, State: "missing", Unpriced: []string{}, Problems: []string{}}
	t, err := Load(o.File)
	switch {
	case err == nil:
		st.State = "custom"
		if t.Managed() {
			st.State = "managed"
		}
		st.Models = len(t.Models)
		if at, e := time.Parse(time.RFC3339, t.FetchedAt); e == nil {
			d := int(o.Now.Sub(at).Hours() / 24)
			st.AgeDays = &d
		}
		for _, m := range ledgerModels(o.WorkDir) {
			if _, ok := t.Lookup(m); !ok {
				st.Unpriced = append(st.Unpriced, m)
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		st.State = "unreadable"
	}
	add := func(f string, a ...any) { st.Problems = append(st.Problems, fmt.Sprintf(f, a...)) }
	switch {
	case st.State == "missing":
		add("no pricing.json: costs are not computed (tokens only)")
	case st.State == "unreadable":
		add("pricing.json is not valid JSON: costs are not computed")
	}
	if st.State == "managed" && st.AgeDays != nil && *st.AgeDays > o.StaleDays {
		add("the price table is %d days old (kit-written): refresh it", *st.AgeDays)
	}
	if len(st.Unpriced) > 0 {
		add("models in your ledgers have no price: %s", strings.Join(st.Unpriced, ", "))
	}
	if o.BaselineModel == "" {
		add("REPORT_BASELINE_MODEL is not set: routing savings stay N/A")
	} else if t != nil {
		if _, ok := exact(t, o.BaselineModel); !ok {
			add("REPORT_BASELINE_MODEL %s is not in the price table: routing savings stay N/A", o.BaselineModel)
		}
	}
	return st
}

func exact(t *Table, m string) (Rate, bool) {
	for _, r := range t.Models {
		if r.Model == m {
			return r, true
		}
	}
	return Rate{}, false
}

// ledgerModels lists the distinct models of ledger lines that have a token record (tq != "unavailable").
func ledgerModels(work string) []string {
	files, _ := filepath.Glob(filepath.Join(work, "*", "usage.jsonl"))
	more, _ := filepath.Glob(filepath.Join(work, "_archive", "*.usage.jsonl"))
	seen := map[string]bool{}
	for _, f := range append(files, more...) {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var l struct{ Model, TQ string }
			if json.Unmarshal(sc.Bytes(), &l) == nil && l.TQ != "unavailable" && l.Model != "" {
				seen[l.Model] = true
			}
		}
		fh.Close()
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Print writes the status the way 1.x did (one line, then one "  - problem" line each); doctor prints the same block.
func (s Status) Print(w io.Writer) {
	age := ""
	if s.AgeDays != nil {
		age = fmt.Sprintf(", %d day(s) old", *s.AgeDays)
	}
	fmt.Fprintf(w, "pricing: %s (%s)%s, %d model(s)\n", s.State, s.File, age, s.Models)
	for _, p := range s.Problems {
		fmt.Fprintln(w, "  - "+p)
	}
}
