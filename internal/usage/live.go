package usage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wecii/sdkit/internal/pricing"
)

// Live is the running total of a ledger read as logged (no second store). Runs counts measured executions; Unmeasured those whose host
// gave no token data (their zeros are not usage).
type Live struct {
	Runs       int `json:"runs"`
	Unmeasured int `json:"unmeasured"`
	In         int `json:"in"`
	Out        int `json:"out"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

// LiveTotals sums the ledger lines of one role ("" = all).
func LiveTotals(file, role string) Live {
	var t Live
	ids, um := map[string]bool{}, map[string]bool{}
	for _, l := range ReadLines(file) {
		if role != "" && l.Role != role {
			continue
		}
		if l.tq() == "unavailable" {
			um[l.ID] = true
			continue
		}
		ids[l.ID] = true
		t.In, t.Out, t.CacheRead, t.CacheWrite = t.In+int(l.In), t.Out+int(l.Out), t.CacheRead+int(l.CacheRead), t.CacheWrite+int(l.CacheWrite)
	}
	t.Runs, t.Unmeasured = len(ids), len(um)
	return t
}

func k(n int) string {
	if n >= 1000 {
		return strconv.FormatFloat(math.Round(float64(n)/100)/10, 'f', -1, 64) + "k"
	}
	return strconv.Itoa(n)
}

// Fragment is the step-line text of one total ("in 41k (fresh 3k, cache read 36k, cache write 2k), out 6.2k, hit 88%").
func (t Live) Fragment() string {
	um := ""
	if t.Unmeasured > 0 {
		um = fmt.Sprintf(", %d run(s) not measured (host gave no token data)", t.Unmeasured)
	}
	if t.Runs == 0 {
		if t.Unmeasured > 0 {
			return "not measured (host gave no token data)"
		}
		return "not measured"
	}
	all, hit := t.In+t.CacheRead+t.CacheWrite, "n/a"
	if all > 0 {
		hit = fmt.Sprintf("%d%%", t.CacheRead*100/all)
	}
	return fmt.Sprintf("in %s (fresh %s, cache read %s, cache write %s), out %s, hit %s%s", k(all), k(t.In), k(t.CacheRead), k(t.CacheWrite), k(t.Out), hit, um)
}

// PricesStatus says which price table is in force and what it covers; the second result is false when the file is not valid JSON.
func PricesStatus(file string, asJSON bool) (string, bool) {
	if _, err := os.Stat(file); err != nil {
		if asJSON {
			return `{"status":"missing","source":null}`, true
		}
		return "pricing: none (" + file + " not found; TELEMETRY_PRICES unset): costs are not computed, tokens are still counted", true
	}
	t, err := pricing.Load(file)
	if err != nil {
		if asJSON {
			return `{"status":"unreadable"}`, false
		}
		return "pricing: " + file + " is not valid JSON: costs are not computed", false
	}
	set := map[string]bool{}
	for _, m := range t.Models {
		set[m.Model] = true
	}
	ids := sortedKeys(set)
	sort.Strings(ids)
	cur, ver := orStr(t.Currency, "USD"), t.Version
	if ver == 0 {
		ver = 2
	}
	if asJSON {
		b, _ := json.Marshal(map[string]any{"status": "ok", "source": file, "version": ver, "currency": cur, "entries": len(t.Models), "models": ids})
		return string(b), true
	}
	return fmt.Sprintf("pricing: %s\n  format version %d, currency %s, %d entr(y/ies), %d model(s): %s\n  USD per million tokens; API-equivalent estimate, never a bill", file, ver, cur, len(t.Models), len(ids), strings.Join(ids, ", ")), true
}
