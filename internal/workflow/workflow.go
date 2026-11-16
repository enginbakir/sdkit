// Package workflow answers what a task's profile and risk require. It only reads; internal/sdd moves the phase.
package workflow

import (
	"fmt"
	"strings"
)

// Profiles in the order they are listed.
var Profiles = []string{"small", "standard", "high-assurance"}

// ProfileLines is `workflow profiles`.
var ProfileLines = []string{
	"small           low risk only; no contract overhead; no mandatory step skipped",
	"standard        the default workflow; contracts optional (CONTRACTS=1)",
	"high-assurance  any risk; independent verifier PASS mandatory at every risk; contracts required for writing/deciding roles",
}

// Check refuses a profile that does not fit the risk (a profile never lowers a gate).
func Check(profile, risk string) error {
	ok := false
	for _, p := range Profiles {
		ok = ok || p == profile
	}
	if !ok {
		return fmt.Errorf("--profile must be one of: %s", strings.Join(Profiles, " "))
	}
	if profile == "small" && risk != "low" {
		return fmt.Errorf("profile small is for low risk only (this task is %s). Use standard or high-assurance; a lighter profile never lowers a gate.", risk)
	}
	return nil
}

// Verifier is what proves the task: the verify command at low risk, an independent verifier otherwise.
func Verifier(risk, profile string) string {
	if risk == "medium" || risk == "high" || profile == "high-assurance" {
		return "independent-verifier"
	}
	return "verify-command"
}

// Contracts is the level (off|optional|required) of a handoff to role under profile; contracts is the CONTRACTS=1 setting.
func Contracts(profile string, contracts bool, role string) string {
	base := role[strings.LastIndex(role, ":")+1:]
	base = strings.TrimSuffix(base, "-deep")
	lvl := "off"
	if contracts {
		lvl = "optional"
	}
	if profile == "high-assurance" {
		switch base {
		case "developer", "test-writer", "devsecops", "engineer", "verifier", "reviewer", "security-auditor", "":
			lvl = "required"
		default:
			if lvl == "off" {
				lvl = "optional"
			}
		}
	}
	return lvl
}

// EvidenceRoles are the roles whose independent PASS the task needs beyond the verify command.
func EvidenceRoles(risk string) []string {
	if risk == "high" {
		return []string{"verifier", "reviewer", "security-auditor"}
	}
	return []string{}
}

// Capability is one row of `workflow capabilities`: what an execution path really enforces.
type Capability struct {
	Path            string `json:"path"`
	SpawnContract   string `json:"spawn_contract"`
	VerdictParse    string `json:"verdict_parse"`
	TrustedIdentity string `json:"trusted_identity"`
	EvidenceRecord  string `json:"evidence_record"`
	StaleDetection  string `json:"stale_detection"`
	CompletionGate  string `json:"completion_gate"`
	FailureReport   string `json:"failure_report"`
}

// Capabilities lists the paths; enforced = refused or recorded by code, detected = noticed later, manual = a person runs the command.
func Capabilities() []Capability {
	return []Capability{
		{"claude-code", "ask (PreToolUse Agent)", "auto (SubagentStop)", "host agent_id + spawn registry", "auto", "enforced", "enforced (sdkit sdd advance)", "systemMessage to the orchestrator"},
		{"sdkit run-role (any host)", "enforced (exit 2, nothing starts)", "auto (after the process ends)", "process-owned run id + spawn registry", "auto", "enforced", "enforced (sdkit sdd advance)", "exit 3/4/5 + stderr"},
		{"host-native subagents (codex, opencode, cursor, gemini)", "none", "manual (sdkit verdict parse --record)", "none: recorded as manual, refused at high risk", "manual", "detected (at advance)", "enforced (sdkit sdd advance)", "refusal at advance"},
		{"git hooks / CI (all hosts)", "none", "none", "attestation review", "from the attestation", "enforced (ci-check)", "enforced (ci-check)", "CI log"},
	}
}
