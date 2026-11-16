package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kit "github.com/wecii/sdkit"
	"github.com/wecii/sdkit/internal/proc"
)

func init() { Register(SessionStart, nil, "check-tools", checkTools) }

func pluginVersion(b []byte, err error) string {
	var p struct{ Version string }
	if err == nil && json.Unmarshal(b, &p) == nil {
		return p.Version
	}
	return ""
}

// checkTools lists the tools the kit needs but cannot find, with the fix, and says when the
// plugin and the binary are different versions.
func checkTools(in *Input) Decision {
	var missing []string
	need := func(name, hint string) {
		if _, err := proc.LookPath(name); err == nil {
			return
		}
		for _, ext := range []string{"", ".cmd"} { // a project-local install counts (node_modules/.bin)
			if _, err := os.Stat(filepath.Join(in.Root, "node_modules", ".bin", name+ext)); err == nil {
				return
			}
		}
		missing = append(missing, fmt.Sprintf("- %s: %s", name, hint))
	}
	need("git", "install git")
	if st := stackOf(in); st != nil {
		for _, t := range st.Tools() {
			need(t.Name, t.Hint)
		}
	}
	if st, err := os.Stat(filepath.Join(in.Root, "openspec")); err == nil && st.IsDir() { // only repos that use OpenSpec (the analyst side)
		need("openspec", "npm install -g @fission-ai/openspec")
	}
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		plug := pluginVersion(os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")))
		bin := pluginVersion(kit.FS.ReadFile(".claude-plugin/plugin.json"))
		if plug != "" && bin != "" && plug != bin {
			missing = append(missing, fmt.Sprintf("- sdkit: the plugin is %s but the sdkit binary is %s; update the older one (see docs/upgrading.md)", plug, bin))
		}
	}
	if len(missing) == 0 {
		return Decision{}
	}
	return Decision{Context: "spec-driven-kit: missing tools, tell the user once:\n" + strings.Join(missing, "\n")}
}
