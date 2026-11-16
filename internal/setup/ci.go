package setup

import "fmt"

// ciFiles maps --ci to the template and the file it becomes in the repo.
var ciFiles = map[string][2]string{
	"bitbucket": {"templates/ci/bitbucket-pipelines.yml", "bitbucket-pipelines.yml"},
	"github":    {"templates/ci/github-actions.yml", ".github/workflows/spec-driven-gate.yml"},
	"gitlab":    {"templates/ci/gitlab-ci.yml", ".gitlab-ci.yml"},
}

// InstallCI writes the server-side gate pipeline of a CI host. It is project-owned: an existing file is kept.
func (s *Setup) InstallCI(host string) error {
	f, ok := ciFiles[host]
	if !ok {
		return fmt.Errorf("--ci takes bitbucket, github or gitlab, got '%s'", host)
	}
	s.say("ci: %s (project-owned: merge into an existing pipeline file by hand)", host)
	return s.InstallTemplate(f[0], f[1])
}
