package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wecii/sdkit/internal/project"
)

func TestActiveSlug(t *testing.T) {
	root := t.TempDir()
	p := project.Project{Root: root, KitDir: ".sdkit", EnvFile: "sdkit.env"}
	mk := func(slug string, age time.Duration) {
		f := Path(p, slug)
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(`{"slug":"`+slug+`"}`), 0o644)
		at := time.Now().Add(-age)
		os.Chtimes(f, at, at)
	}
	if got := ActiveSlug(p); got != "" {
		t.Errorf("nothing yet: %q", got)
	}
	mk("old", time.Hour)
	mk("new", time.Minute)
	cases := []struct {
		name, task, want string
	}{
		{"unset: the newest work state, as before", "", "new"},
		{"a named task wins, even with no state of its own", "odeme-akisi", "odeme-akisi"},
		{"a name with a dot is fine", "FORGOT.PASSWORD-v2", "FORGOT.PASSWORD-v2"},
		{"a name that is not a slug is ignored", "../x", "new"},
	}
	for _, c := range cases {
		q := p
		q.Task = c.task
		if got := ActiveSlug(q); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
