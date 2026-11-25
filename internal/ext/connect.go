package ext

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/wecii/sdkit/internal/ui"
)

// ConnectResult is what saving the connection answers: what was stored and what each service said to the credentials.
type ConnectResult struct {
	Saved     bool      `json:"saved"`
	Jira      ui.Check  `json:"jira"`
	Bitbucket *ui.Check `json:"bitbucket,omitempty"`
}

// WriteConnection prints the saved connection; a token is never printed, only whether one is set.
func WriteConnection(w io.Writer, root string) error {
	return json.NewEncoder(w).Encode(ui.ReadConnection(root))
}

// SaveConnection reads a ui.ConnectionInput from in, stores it in the personal settings file (the same place the panel uses, so the
// kit's own commands see the tokens), checks the credentials and prints the result. The tokens arrive on the standard input, never
// on a command line, where other programs could read them.
func SaveConnection(ctx context.Context, w io.Writer, in io.Reader, root, bitbucketAPI string, hc *http.Client) error {
	var input ui.ConnectionInput
	if err := json.NewDecoder(io.LimitReader(in, 1<<16)).Decode(&input); err != nil {
		return err
	}
	if err := ui.SaveConnection(root, input); err != nil {
		return err
	}
	if hc == nil {
		hc = ui.HTTPClient()
	}
	if bitbucketAPI == "" {
		bitbucketAPI = "https://api.bitbucket.org/2.0"
	}
	jt, bt := ui.SavedTokens(root)
	out := ConnectResult{Saved: true, Jira: ui.Check{Message: "Jira token'ı henüz girilmedi"}}
	if jt != "" {
		c := ui.ReadConnection(root)
		out.Jira = ui.CheckJira(ctx, hc, c.JiraBaseURL, c.JiraUser, jt)
	}
	if bt != "" {
		c := ui.CheckBitbucket(ctx, hc, bitbucketAPI, ui.Origin(root), ui.ReadConnection(root).BitbucketEmail, bt)
		out.Bitbucket = &c
	}
	return json.NewEncoder(w).Encode(out)
}
