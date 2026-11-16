package setup

import (
	"io"
	"testing"
)

func TestUpgradeRemovesOnlyKitOwned(t *testing.T) {
	s := New(t.TempDir())
	s.Out = io.Discard
	for _, f := range []string{"sdkit.env", "scripts/env.sh", "scripts/edited.sh", "scripts/own.sh"} {
		put(s.abs(".sdkit/"+f), []byte(f), 0o644)
	}
	put(s.abs(".sdkit/.scaffold-manifest"), []byte(".sdkit/scripts/env.sh\tx\t"+shaFile(s.abs(".sdkit/scripts/env.sh"))+"\n.sdkit/scripts/edited.sh\tx\tdead\n"), 0o644)
	s.Upgrade(nil)
	if exists := func(f string) bool { return shaFile(s.abs(".sdkit/scripts/"+f)) != shaFile("") }; exists("env.sh") || !exists("edited.sh") || !exists("own.sh") {
		t.Error("upgrade must remove only the unedited kit-owned file")
	}
}
