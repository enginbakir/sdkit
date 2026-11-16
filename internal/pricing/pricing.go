// Package pricing owns <project-dir>/pricing.json, the one table every cost
// figure comes from (USD per million tokens, API-equivalent, never a bill).
// A model that is not in the table has no price: callers print N/A, never a guess.
package pricing

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// Marker in "managed_by" of a table this package wrote; only such a table is replaced without force.
const Marker = "sdkit pricing"

// Rate is the price of one model, USD per million tokens. A zero cache rate means none is listed.
type Rate struct {
	Model      string  `json:"model"`
	Provider   string  `json:"provider,omitempty"`
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	Source     string  `json:"source,omitempty"`
}

// Table is the file.
type Table struct {
	Version       int    `json:"version"`
	Currency      string `json:"currency"`
	ManagedBy     string `json:"managed_by,omitempty"`
	FetchedAt     string `json:"fetched_at,omitempty"`
	PriceListDate string `json:"price_list_date,omitempty"`
	Note          string `json:"note,omitempty"`
	Models        []Rate `json:"models"`
}

// Load reads a table; the error says whether it is missing or unreadable.
func Load(path string) (*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

var dated = regexp.MustCompile(`-[0-9]{8}$`)

// Lookup resolves a model id: exact, then without a trailing -YYYYMMDD. ok is false for an unknown model (unavailable).
func (t *Table) Lookup(model string) (Rate, bool) {
	if t == nil || model == "" {
		return Rate{}, false
	}
	for _, id := range []string{model, dated.ReplaceAllString(model, "")} {
		for _, r := range t.Models {
			if r.Model == id {
				return r, true
			}
		}
	}
	return Rate{}, false
}

// Managed reports whether the kit wrote the table.
func (t *Table) Managed() bool { return strings.TrimSpace(t.ManagedBy) == Marker }
