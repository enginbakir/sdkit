package sdd

import (
	"strings"
	"testing"

	"github.com/wecii/sdkit/internal/plan"
)

func TestScenarioSummaries(t *testing.T) {
	spec := "#### Scenario: AC-X-1\n- **WHEN** user taps | submit\n- **THEN** one request is sent\n\n" +
		"#### Scenario: AC-X-2 (withdrawn)\n- **WHEN** old\n\n### Requirement: next\n#### Scenario: AC-X-3 (pending Q1)\n- **THEN** shown\n"
	got := scenarioSummaries(spec)
	if len(got) != 2 || got["AC-X-2"] != "" {
		t.Fatalf("withdrawn must be skipped: %v", got)
	}
	if got["AC-X-1"] != "WHEN user taps / submit; THEN one request is sent" {
		t.Fatalf("%q", got["AC-X-1"])
	}
	// a Definition of Done seeded from it still passes dod check (the "|" of the text must not read as Verified-by)
	dod := "## Definition of Done\n\n- D1: AC-X-1: " + got["AC-X-1"] + " | Verified by: automated test\n"
	if out, ok := plan.CheckDoD(dod); !ok {
		t.Fatal(strings.Join(out, "; "))
	}
}
