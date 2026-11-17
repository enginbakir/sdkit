package jira

import (
	"os"
	"regexp"
	"strings"
)

// Fetch is the issue as normalised task JSON.
func (c *Config) Fetch(key string) (Task, error) {
	iss, err := c.Get(key)
	if err != nil {
		return nil, err
	}
	return Normalise(iss)
}

// DepsReady reports the dependencies of key that are not in the done status category.
func (c *Config) DepsReady(key string) (pending []map[string]any, err error) {
	t, err := c.Fetch(key)
	if err != nil {
		return nil, err
	}
	deps, _ := sarr(t["dependencies"])
	pending = []map[string]any{}
	for _, d := range deps {
		di, err := c.Get(d)
		if err != nil {
			return nil, err
		}
		if dig(di, "fields", "status", "statusCategory", "key") != "done" {
			st, ok := dig(di, "fields", "status", "name").(string)
			if !ok {
				st = "?"
			}
			pending = append(pending, map[string]any{"key": d, "status": st})
		}
	}
	return pending, nil
}

func (c *Config) project(fallback string) string {
	if c.Project != "" {
		return c.Project
	}
	k, _, _ := strings.Cut(fallback, "-")
	return k
}

// Upsert creates or updates the subtask labelled sdkit-ext-<ext>; it never sets an assignee. warn gets non-fatal notes.
func clip(s string) string {
	if s = strings.TrimSpace(s); len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

func (c *Config) Upsert(ext, in string, warn func(string)) (key, action string, err error) {
	t, found, obj := TaskFrom(in)
	if !found || !obj {
		if st, e := os.Stat(strings.TrimSpace(in)); e == nil && !st.IsDir() {
			return "", "", fail(4, "task JSON is not an object: %q is a file path (pass the JSON text itself or use an object, not a path)", strings.TrimSpace(in))
		}
		return "", "", fail(4, "task JSON is not an object: expected the output of `sdkit tasks render <tasks.json> <ref> --json` (a JSON object with summary, description and the contract fields), got %q", clip(in))
	}
	if err = Check(t); err != nil {
		return
	}
	if t["externalId"] != ext {
		return "", "", fail(4, "externalId argument does not match the JSON")
	}
	lbl, parent := Label(ext), t["parent"].(string)
	summary := ext
	if s, ok := t["summary"]; ok && s != nil {
		summary = strings.Trim(JSON(s, ""), `"`)
		if str, ok := s.(string); ok {
			summary = str
		}
	}
	note, _ := t["description"].(string)
	desc := APIDesc(Desc(t, note), c.Version)
	found2, err := c.Search(`labels = "` + lbl + `"`)
	if err != nil {
		return
	}
	issues, _ := found2["issues"].([]any)
	for _, x := range issues {
		m, _ := x.(map[string]any)
		labels, _ := dig(m, "fields", "labels").([]any)
		for _, l := range labels {
			if l == lbl && key == "" {
				key, _ = m["key"].(string)
			}
		}
	}
	if key != "" {
		if len(issues) > 1 {
			warn("warning: several issues carry " + lbl + "; updating " + key)
		}
		return key, "updated", c.Update(key, map[string]any{"summary": summary, "description": desc})
	}
	// A role subtask hangs under its group task, so its parent must already be a Jira key (the group is created first).
	if !reKey.MatchString(parent) {
		return "", "", fail(4, "parent %q is not a Jira key: create the parent task first and pass --keys to render", parent)
	}
	labels, typ := []string{lbl, "sdkit"}, c.SubtaskType
	if a, _ := t["area"].(string); a != "" {
		labels = append(labels, "sdkit-area-"+a)
	}
	if g, _ := t["group"].(bool); g {
		typ, labels = c.TaskType, append(labels, "sdkit-group")
	}
	key, err = c.Create(map[string]any{"project": map[string]any{"key": c.project(parent)}, "issuetype": map[string]any{"name": typ},
		"parent": map[string]any{"key": parent}, "summary": summary, "description": desc, "labels": labels})
	return key, "created", err
}

var reCRKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

// ChangeRequest creates a spec-change issue linked as blocking key and returns its key.
func (c *Config) ChangeRequest(key, in string) (string, error) {
	t, _, obj := TaskFrom(in)
	if !obj || !nes(t["reason"]) {
		return "", fail(4, "change request needs a JSON object with a non-empty reason")
	}
	if !reCRKey.MatchString(key) {
		return "", fail(2, "bad issue key")
	}
	reason := t["reason"].(string)
	blk := map[string]any{"blocked": key, "reason": reason, "section": t["section"], "proposal": t["proposal"], "specId": t["specId"], "specSha": t["specSha"]}
	first, _, _ := strings.Cut(reason, "\n")
	if r := []rune(first); len(r) > 120 {
		first = string(r[:120])
	}
	desc := "Spec change requested by sdkit while implementing " + key + ". Implementation is blocked until the spec is revised and approved.\n\n```sdkit-change-request\n" + JSON(blk, "  ") + "\n```\n"
	crk, err := c.Create(map[string]any{"project": map[string]any{"key": c.project(key)}, "issuetype": map[string]any{"name": c.ChangeType},
		"summary": "[spec-change] " + key + ": " + first, "description": APIDesc(desc, c.Version), "labels": []string{"sdkit", "sdkit-spec-change", "sdkit-blocked-" + key}})
	if err != nil {
		return "", err
	}
	if err := c.Link(c.LinkType, key, crk); err != nil {
		return "", fail(5, "change request %s was created but could not be linked to %s", crk, key)
	}
	return crk, nil
}

// Supersede marks old subtasks as replaced by another: a comment on each, then the transition to JIRA_SUPERSEDED_STATUS when
// that is set. Nothing is deleted. With !apply it only returns what it would do (a person confirms first).
func (c *Config) Supersede(old []string, by string, apply bool) (plan []string, err error) {
	if !reKey.MatchString(by) {
		return nil, fail(2, "--by %q is not a Jira key (e.g. PROJ-123)", by)
	}
	for _, k := range old {
		if !reKey.MatchString(k) {
			return nil, fail(2, "%q is not a Jira key (e.g. PROJ-123)", k)
		}
		if k == by {
			return nil, fail(2, "%s cannot supersede itself", k)
		}
		line := k + ": comment \"superseded by " + by + "\""
		if c.SupersededStatus != "" {
			line += ", then move to " + c.SupersededStatus
		}
		plan = append(plan, line)
	}
	if !apply {
		return plan, nil
	}
	for _, k := range old {
		if err := c.Comment(k, "Superseded by "+by+": its acceptance criteria moved there when the task plan was regrouped. Nothing was deleted."); err != nil {
			return plan, err
		}
		if c.SupersededStatus != "" {
			if err := c.Transition(k, c.SupersededStatus); err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}
