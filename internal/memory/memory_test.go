package memory

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestAddKeepsOneIndexLineEach(t *testing.T) {
	m := &Memory{Root: t.TempDir(), KitDir: "k", Get: func(k string) string { return map[string]string{"MEMORY": "1"}[k] }, Out: io.Discard, Err: io.Discard, Now: time.Now()}
	m.Run([]string{"add", "fact", "a", "--title", "A"})
	m.Run([]string{"add", "howto", "b", "--title", "B"})
	if strings.Count(m.Autoload(), "\n- [") != 2 {
		t.Fatal(m.Autoload())
	}
}
