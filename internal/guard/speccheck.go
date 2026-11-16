package guard

import (
	"strings"

	"github.com/wecii/sdkit/internal/spec"
)

type specChecker struct{}

// Check runs `spec check <id>`; the report is returned trimmed and a non-mergeable spec is the error.
func (specChecker) Check(root, id string) (string, error) {
	rep, err := spec.Check(root, id)
	return strings.TrimRight(rep, "\n"), err
}

func init() { Specs = specChecker{} }
