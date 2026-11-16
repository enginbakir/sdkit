package hook

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	kit "github.com/wecii/sdkit"
)

func init() {
	Register(PreToolUse, []string{"Bash", "Edit", "Write", "NotebookEdit"}, "guard-readonly", guardReadonly)
}

var (
	roDevNull   = regexp.MustCompile(`[0-9&]?>+[[:space:]]*/dev/null|[0-9]>&[0-9]`)
	roSplit     = regexp.MustCompile(`&&|\|\||;|\||\n`)
	roLead      = regexp.MustCompile(`^[[:space:]]+`)
	roPrefix    = regexp.MustCompile(`^(env |sudo |command |time )`)
	roFileCmd   = regexp.MustCompile(`^(rm|rmdir|mv|cp|mkdir|touch|chmod|chown|ln|tee|dd|truncate|install|patch) `)
	roInPlace   = regexp.MustCompile(`^(sed .*-i|perl .*-i|sed -[a-zA-Z]*)`) // the last branch also catches sed -n: kept from the bash
	roGit       = regexp.MustCompile(`^git (add|commit|checkout|switch|reset|restore|clean|stash|merge|rebase|cherry-pick|apply|push|pull|fetch|worktree|rm|mv|tag|branch -[dDmM])`)
	roInstaller = regexp.MustCompile(`^(npm install|npm i |pnpm install|pnpm add|yarn add|yarn install|pip install|go get|go mod tidy)`)
)

// roleTools reads the tools line of the role's agent file (project copy, else the kit's);
// found is false when there is no such file.
func roleTools(root, name string) (tools string, found bool) {
	b, err := os.ReadFile(filepath.Join(root, ".claude", "agents", name+".md"))
	if err != nil {
		b, err = kit.FS.ReadFile("agents/" + name + ".md")
	}
	if err != nil {
		return "", false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "tools:") {
			return l, true
		}
	}
	return "", true
}

// guardReadonly keeps a read-only role (an agent file whose tools lack Edit and Write)
// from changing files. No agent type in the input means off.
func guardReadonly(in *Input) Decision {
	if in.AgentType == "" {
		return Decision{}
	}
	name := in.AgentType[strings.LastIndex(in.AgentType, ":")+1:]
	tools, found := roleTools(in.Root, name)
	if !found || strings.Contains(tools, "Edit") || strings.Contains(tools, "Write") {
		return Decision{}
	}
	deny := func(why string) Decision { return Decision{Kind: Deny, Reason: "'" + name + "' is read-only: " + why} }
	switch in.Tool {
	case "Edit", "Write", "NotebookEdit":
		return Decision{Kind: Deny, Reason: "'" + name + "' is a read-only role: it cannot edit files. Report the finding instead."}
	case "Bash":
	default:
		return Decision{}
	}
	scrubbed := roDevNull.ReplaceAllString(in.Command, "")
	if strings.Contains(scrubbed, ">") {
		return deny("output redirection to a file is not allowed.")
	}
	for _, seg := range roSplit.Split(scrubbed, -1) {
		seg = roPrefix.ReplaceAllString(roLead.ReplaceAllString(seg, ""), "")
		switch {
		case roFileCmd.MatchString(seg):
			return deny("'" + strings.SplitN(seg, " ", 2)[0] + "' changes files.")
		case roInPlace.MatchString(seg):
			return deny("in-place edit is not allowed.")
		case roGit.MatchString(seg):
			return deny("'" + seg + "' changes the repository.")
		case roInstaller.MatchString(seg):
			return deny("installing dependencies changes files.")
		}
	}
	return Decision{}
}
