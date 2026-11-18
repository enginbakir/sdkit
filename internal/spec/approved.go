package spec

import (
	"strconv"
	"strings"
)

// Commit is a point on the base branch where a spec stands approved: what generate-tasks may be given as its SHA.
type Commit struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	Version string `json:"version"`
}

// ApprovedCommits lists, newest first, the commits of origin/<base> that changed the spec's meta and leave it approved: the SHAs
// `sdkit spec approved-sha` would accept (a spec with open changes is still refused later, by that gate). Nothing is fetched or
// written; the caller decides whether origin is fresh. At most limit commits are looked at.
func ApprovedCommits(root, id string, limit int) []Commit {
	if !okname(id) {
		return nil
	}
	e := envAt(root, nil, nil)
	if !e.gok("show-ref", "--verify", "-q", "refs/remotes/origin/"+e.base) {
		return nil
	}
	out, err := e.g("log", "origin/"+e.base, "-n", strconv.Itoa(limit), "--format=%H%x09%cs%x09%s", "--", e.dir(id)+"/spec.meta.json")
	if err != nil {
		return nil
	}
	var cs []Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		m, ok := e.show(f[0], e.dir(id)+"/spec.meta.json")
		if !ok || jsonStr(m, "status") != "approved" {
			continue
		}
		cs = append(cs, Commit{SHA: f[0], Short: f[0][:7], Date: f[1], Subject: f[2], Version: jsonStr(m, "version")})
	}
	return cs
}
