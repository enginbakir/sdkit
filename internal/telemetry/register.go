package telemetry

import "github.com/wecii/sdkit/internal/sdd"

// sink is the sdd.Telemetry of the binary: every event goes to the open project's log, and a failure is dropped.
type sink struct{}

func (sink) Emit(typ string, f ...string) {
	defer func() { _ = recover() }()
	_ = Emit(sdd.Root(), typ, f, false, false)
}

func init() { sdd.Tel = sink{} }
