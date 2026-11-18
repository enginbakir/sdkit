package secretscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// BaselineFile is the name of the committed list of accepted findings inside the project directory.
const BaselineFile = "secret-baseline.json"

// Entry accepts one finding by fingerprint. Reason is required: an acceptance nobody explained is not an acceptance.
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule,omitempty"`
	Path        string `json:"path,omitempty"`
	Reason      string `json:"reason"`
}

// Baseline is the file content.
type Baseline struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// LoadBaseline reads path. A missing file is an empty baseline; an unreadable or invalid one is an error (never silently ignored,
// or a typo would make every accepted finding reappear, or worse hide none).
func LoadBaseline(path string) (Baseline, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Baseline{}, nil
	}
	if err != nil {
		return Baseline{}, err
	}
	var bl Baseline
	if err := json.Unmarshal(b, &bl); err != nil {
		return Baseline{}, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range bl.Entries {
		if e.Fingerprint == "" || e.Reason == "" {
			return Baseline{}, fmt.Errorf("%s: entry %d needs a fingerprint and a reason", path, i+1)
		}
	}
	return bl, nil
}

// Split separates findings the baseline accepts from the open ones.
func (b Baseline) Split(all []Finding) (open, accepted []Finding) {
	ok := map[string]bool{}
	for _, e := range b.Entries {
		ok[e.Fingerprint] = true
	}
	for _, f := range all {
		if ok[f.Fingerprint] {
			accepted = append(accepted, f)
		} else {
			open = append(open, f)
		}
	}
	return
}
