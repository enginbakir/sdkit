package design

import "testing"

// Decision: a design made right after its spec joins the spec's open pull request; anywhere else it has a branch and pull request of its own.
func TestPublishBranch(t *testing.T) {
	for _, c := range []struct {
		cur, id, branch string
		withSpec        bool
	}{
		{"spec/login", "login", "spec/login", true},
		{"main", "login", "design/login", false},
		{"design/login", "login", "design/login", false},
		{"spec/other", "login", "design/login", false},           // another spec's branch is not this spec's pull request
		{"spec/login--change-1", "login", "design/login", false}, // a change of the spec has its own pull request: not joined
		{"", "login", "design/login", false},
	} {
		if b, w := publishBranch(c.cur, c.id); b != c.branch || w != c.withSpec {
			t.Errorf("publishBranch(%q, %q) = %q, %v; want %q, %v", c.cur, c.id, b, w, c.branch, c.withSpec)
		}
	}
}
