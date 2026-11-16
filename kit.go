// Package kit embeds the kit's data files into the sdkit binary.
package kit

import "embed"

// FS holds the kit data; .claude-plugin/plugin.json is named explicitly
// because embed skips dot directories.
//
//go:embed core skills agents adapters stacks templates .claude-plugin/plugin.json
var FS embed.FS
