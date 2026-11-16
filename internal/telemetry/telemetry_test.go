package telemetry

import "testing"

func TestSchema(t *testing.T) {
	if len(ValidateLine([]byte(`{"v":1,"ts":"2026-01-01T00:00:00Z","type":"phase.reject","task":"a","run":"r","mode":"live","reason":"nope"}`))) != 1 {
		t.Fatal("unknown reason must be the only error")
	}
}
