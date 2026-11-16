package glob

import "testing"

func TestMatchList(t *testing.T) {
	for _, c := range []struct {
		path, globs string
		want        bool
	}{
		{"a/b.pem", "*.pem", true}, {".env.local", ".env*", true}, {"x/.env", ".env*", true},
		{"a/secrets/k.txt", "**/secrets/*", true}, {"secrets/k", "**/secrets/*", false},
		{"secrets/k/z", "secrets/*", true}, {"a/b.go", "*.go", true}, {"a/b.go", "a/*.go", true},
		{"a/b.go", "b", false}, {"my dir/a b.go", "*b.go", true}, {"a.txt", ",,*.go,", false},
		{"a.txt", "", false}, {"b", "[abc]", true}, {"d", "[abc]", false}, {"d", "[!abc]", true},
		{"b", "[a-c]", true}, {"a[", "a[", true}, {"ab", "a[]]", false}, {"a]", "a[]]", true},
		{"a*", `a\*`, true}, {"ab", `a\*`, false}, {"ab", "a?", true}, {"A.PEM", "*.pem", false},
	} {
		if got := MatchList(c.path, c.globs, false); got != c.want {
			t.Errorf("MatchList(%q,%q)=%v", c.path, c.globs, got)
		}
	}
	if !MatchList("A.PEM", "*.pem", true) || !HasPrefix("src/x", "x,src/", false) || HasDirPrefix("srcx", "src", false) {
		t.Error("fold/prefix")
	}
}
