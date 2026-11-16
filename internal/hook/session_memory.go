package hook

import (
	"github.com/wecii/sdkit/internal/memory"
	"github.com/wecii/sdkit/internal/project"
)

func init() { Register(SessionStart, nil, "session-memory", sessionMemory) }

// sessionMemory injects the project memory index (MEMORY=1), one line per entry.
func sessionMemory(in *Input) Decision {
	if setting(in, "MEMORY") != "1" {
		return Decision{}
	}
	m := &memory.Memory{Root: in.Root, KitDir: project.Resolve(in.Root).KitDir, Get: func(k string) string { return setting(in, k) }}
	if idx := m.Autoload(); idx != "" {
		return Decision{Context: "Project memory index (open an entry only when its line is relevant; add with sdkit memory add): " + idx}
	}
	return Decision{}
}
