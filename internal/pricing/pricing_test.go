package pricing

import "testing"

func TestLookup(t *testing.T) {
	tb := &Table{Models: []Rate{{Model: "claude-a", In: 3, Out: 15}}}
	for m, want := range map[string]bool{"claude-a": true, "claude-a-20250101": true, "claude-b": false, "": false} {
		if _, ok := tb.Lookup(m); ok != want {
			t.Errorf("%q: got %v", m, ok)
		}
	}
}
