package hook

import "github.com/wecii/sdkit/internal/sdd"

func init() {
	Register(PreToolUse, []string{"Edit", "Write"}, "guard-phase", guardPhase)
}

// guardPhase lets code change only in the phase that allows it (the rule is sdd.Env.Gate).
// SDD_MODE: strict denies, guided asks, off disables.
func guardPhase(in *Input) Decision {
	if in.Path == "" {
		return Decision{}
	}
	e := sdd.New(in.Root)
	mode := e.Mode()
	if mode == "off" || !e.P.Found { // not a kit project: nothing to enforce (the bash denied here too)
		return Decision{}
	}
	ok, why := e.Gate("edit", in.Path)
	return refusal(ok, why, mode)
}

func refusal(ok bool, why, mode string) Decision {
	switch {
	case ok:
		return Decision{}
	case mode == "strict":
		return Decision{Kind: Deny, Reason: why}
	}
	return Decision{Kind: Ask, Reason: why}
}
