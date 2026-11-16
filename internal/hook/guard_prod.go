package hook

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
)

func init() { Register(PreToolUse, []string{"Bash"}, "guard-prod", guardProd) }

var (
	prodMutating = regexp.MustCompile(`kubectl([^|;&]*) (apply|delete|rollout|scale|patch|edit|replace|create|run|set|annotate|label|exec|cp|drain|cordon|taint)( |$)`)
	prodContext  = regexp.MustCompile(`--context[= ]([^ ]+)`)
)

func kubectl(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := proc.Run(ctx, root, nil, "kubectl", args...)
	if err != nil || r.Code != 0 {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

// guardProd asks before state-changing kubectl against a cluster listed in PROD_KUBE_CLUSTERS.
func guardProd(in *Input) Decision {
	list := in.Settings().String("PROD_KUBE_CLUSTERS")
	if list == "" || !strings.Contains(in.Command, "kubectl") || !prodMutating.MatchString(in.Command) {
		return Decision{}
	}
	target := ""
	if m := prodContext.FindStringSubmatch(in.Command); m != nil { // an explicit --context wins
		target = m[1]
	} else {
		target = kubectl(in.Root, "config", "current-context")
	}
	cluster := kubectl(in.Root, "config", "view", "-o", "jsonpath={.contexts[?(@.name=='"+target+"')].context.cluster}")
	for _, prod := range strings.Split(list, ",") {
		if prod != "" && strings.HasSuffix(cluster, prod) {
			return Decision{Kind: Ask, Reason: "Mutating kubectl against PRODUCTION cluster '" + prod + "' (context '" + target + "'). Confirm this is intended."}
		}
	}
	return Decision{}
}
