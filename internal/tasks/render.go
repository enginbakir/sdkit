package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Contract is the task contract: the fields of the fenced sdkit-task block, in the 1.x order.
type Contract struct {
	ExternalID   string   `json:"externalId"`
	SpecID       string   `json:"specId"`
	SpecVersion  any      `json:"specVersion"`
	SpecSha      string   `json:"specSha"`
	ACs          []string `json:"acceptanceCriteria"`
	Risk         any      `json:"risk"`
	Dependencies []string `json:"dependencies"`
	Scope        []string `json:"scope"`
	Parent       string   `json:"parent"`
}

// Upsert is the rendered task: the contract plus the Jira summary and description.
type Upsert struct {
	Contract
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// Render builds the upsert of one ref. keys maps externalId to a Jira key; an unmapped
// dependency stays its externalId. ok is false for an unknown ref.
func (d *Doc) Render(ref string, keys map[string]string) (u Upsert, ok bool) {
	var t *Task
	byRef := map[string]string{}
	for i := range d.Tasks {
		byRef[d.Tasks[i].Ref] = d.Tasks[i].ExternalID
		if d.Tasks[i].Ref == ref {
			t = &d.Tasks[i]
		}
	}
	if t == nil {
		return u, false
	}
	u.Contract = Contract{ExternalID: t.ExternalID, SpecID: d.SpecID, SpecVersion: d.SpecVersion, SpecSha: d.SpecSha, ACs: t.ACs,
		Risk: t.Risk, Dependencies: []string{}, Scope: t.Scope}
	u.Description = Str(t.Summary)
	if len(t.BlockedBy) > 0 {
		u.Description = "Blocked: " + strings.Join(t.BlockedBy, ", ") + "\n\n" + u.Description
	}
	if p, _ := d.Parent.(string); p != "" {
		u.Parent = p
	}
	if t.Title == nil || t.Title == false {
		u.Summary = t.ExternalID
	} else {
		u.Summary = Str(t.Title)
	}
	for _, dep := range t.DependsOn {
		if e, found := byRef[dep]; found {
			if k := keys[e]; k != "" {
				e = k
			}
			u.Dependencies = append(u.Dependencies, e)
		}
	}
	return u, true
}

// Text is the Jira description: the summary text and the fenced sdkit-task block.
func (u Upsert) Text() string {
	b, _ := marshal(u.Contract)
	var pretty bytes.Buffer
	_ = json.Indent(&pretty, b, "", "  ")
	return u.Description + "\n\n```sdkit-task\n" + pretty.String() + "\n```\n"
}

// JSON is the one-line upsert (render --json).
func (u Upsert) JSON() string {
	b, _ := marshal(u)
	return string(b)
}

// Report renders the creation report from the results (externalId -> result object).
// ok is false when a task has no result or a row says NOT CREATED.
func (d *Doc) Report(results map[string]map[string]any) (md string, ok bool) {
	s := func(v any) string {
		if str, isStr := v.(string); isStr {
			return str
		}
		if v == nil {
			return "null"
		}
		return Str(v)
	}
	or := func(v any, alt string) string {
		if v == nil || v == false {
			return alt
		}
		return s(v)
	}
	var l []string
	add := func(x ...string) { l = append(l, x...) }
	add(fmt.Sprintf("# Task creation report: %s v%s @ %s\n\nParent: %s\n", d.SpecID, s(d.SpecVersion), d.SpecSha, s(d.Parent)),
		"## Results\n\n| Task | externalId | Jira | Result |\n|---|---|---|---|")
	var miss []string
	for _, t := range d.Tasks {
		r, has := results[t.ExternalID]
		if !has {
			miss = append(miss, t.Ref)
		}
		add(fmt.Sprintf("| %s | %s | %s | %s |", t.Ref, t.ExternalID, or(r["key"], "-"), or(r["action"], "NOT CREATED")))
	}
	add("\n## Acceptance criteria to tasks\n")
	byAC := map[string][]string{}
	var acs []string
	for _, t := range d.Tasks {
		for _, ac := range t.ACs {
			if byAC[ac] == nil {
				acs = append(acs, ac)
			}
			byAC[ac] = append(byAC[ac], t.Ref)
		}
	}
	for _, ac := range sorted(acs) {
		add("- " + ac + ": " + strings.Join(byAC[ac], ", "))
	}
	add("\n## Task dependencies\n")
	for _, t := range d.Tasks {
		deps := "none"
		if len(t.DependsOn) > 0 {
			deps = strings.Join(t.DependsOn, ", ")
		}
		add("- " + t.Ref + " <- " + deps)
	}
	if len(miss) > 0 {
		add("\n## FAILED\n\nNo result for: " + strings.Join(miss, ", ") + ". This run is NOT a success.\n")
	} else {
		add("")
	}
	md = strings.TrimRight(strings.Join(l, "\n"), "\n")
	return md, !strings.Contains(md, "NOT CREATED")
}
