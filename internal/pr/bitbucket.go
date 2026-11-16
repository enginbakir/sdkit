package pr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
)

type credentials struct{ token, email string }

// header: ATCTT... access tokens are Bearer, anything else is Basic with the account email.
func (c credentials) header() string {
	if strings.HasPrefix(c.token, "ATCTT") {
		return "Bearer " + c.token
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token))
}

func (r *Request) api() string {
	if a := r.env("PR_API_BASE"); a != "" {
		return strings.TrimRight(a, "/")
	}
	return "https://api.bitbucket.org/2.0"
}

var client = &http.Client{Timeout: 60 * time.Second}

// call sends one request and returns status and body; status 0 means the host was unreachable.
func (r *Request) call(ctx context.Context, c credentials, method, url string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", c.header())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func (r *Request) checkCredentials(ctx context.Context, slug, token, email string) int {
	code, _, _ := r.call(ctx, credentials{token, email}, "GET", r.api()+"/repositories/"+slug, nil)
	return code
}

func trunc(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

type found struct {
	ID    int
	Links struct{ HTML struct{ Href string } }
}

func (r *Request) bitbucket(ctx context.Context, source, desc string) error {
	creds := reCreds.FindStringSubmatch(r.tokenURL())
	token := r.env("BITBUCKET_TOKEN")
	if token == "" && creds == nil {
		return fail(1, "no token: the origin URL carries none; set BITBUCKET_TOKEN or PR_TOKEN_REPO (see docs/hosts.md)")
	}
	if token == "" {
		token = creds[1][strings.Index(creds[1], ":")+1:]
	}
	email := r.env("BITBUCKET_EMAIL")
	if email == "" {
		email = gitx.Config(r.Dir, "user.email")
	}
	if !strings.HasPrefix(r.env("BITBUCKET_TOKEN"), "ATCTT") && email == "" {
		return fail(2, "set BITBUCKET_EMAIL or git config user.email (the Bitbucket account email)")
	}
	origin, _ := gitx.Run(r.Dir, "remote", "get-url", "origin")
	m := reRepo.FindStringSubmatch(origin)
	if m == nil {
		return fail(1, "origin is not a Bitbucket URL")
	}
	c, base := credentials{token, email}, r.api()+"/repositories/"+m[1]+"/pullrequests"
	lookup := func(what string) (*found, error) {
		q := queryEscape(fmt.Sprintf(`source.branch.name="%s" AND state="OPEN"`, source))
		code, b, err := r.call(ctx, c, "GET", base+"?q="+q, nil)
		if err != nil {
			return nil, fail(1, "%v", err)
		}
		if code >= 400 || code == 0 {
			return nil, fail(1, "HTTP %d while looking for %s: %s", code, what, trunc(b))
		}
		var v struct{ Values []found }
		_ = json.Unmarshal(b, &v)
		if len(v.Values) == 0 {
			return nil, nil
		}
		return &v.Values[0], nil
	}
	send := func(method, url string, payload any) ([]byte, error) {
		code, b, err := r.call(ctx, c, method, url, payload)
		if err != nil {
			return nil, fail(1, "%v", err)
		}
		if code >= 400 || code == 0 {
			hint := ""
			if code == 401 && method == "POST" && url == base {
				hint = " (check that BITBUCKET_EMAIL is the Bitbucket account email)"
			}
			return nil, fail(1, "HTTP %d: %s%s", code, trunc(b), hint)
		}
		return b, nil
	}
	if r.Mode == "update" || r.Mode == "close" {
		pr, err := lookup("the PR")
		if err != nil {
			return err
		}
		if pr == nil {
			return fail(1, "no open PR for branch %s", source)
		}
		href, one := pr.Links.HTML.Href, fmt.Sprintf("%s/%d", base, pr.ID)
		if r.Mode == "update" {
			if _, err := send("PUT", one, map[string]string{"title": r.Title, "description": desc}); err != nil {
				return err
			}
			fmt.Fprintln(r.Out, href)
			return nil
		}
		if _, err := send("POST", one+"/comments", map[string]any{"content": map[string]string{"raw": desc}}); err != nil {
			return err
		}
		if _, err := send("POST", one+"/decline", map[string]any{}); err != nil {
			return err
		}
		fmt.Fprintf(r.Out, "closed %s: %s\n", source, href)
		return nil
	}
	if r.Reuse {
		pr, err := lookup("an existing PR")
		if err != nil {
			return err
		}
		if pr != nil {
			fmt.Fprintln(r.Out, pr.Links.HTML.Href)
			return nil
		}
	}
	b, err := send("POST", base, CreateBody(r.Title, source, r.Dest, desc))
	if err != nil {
		return err
	}
	var v struct {
		ID          int
		State       string
		Destination struct{ Branch struct{ Name string } }
		Links       struct{ HTML struct{ Href string } }
	}
	_ = json.Unmarshal(b, &v)
	fmt.Fprintf(r.Out, "PR #%d %s -> %s: %s\n", v.ID, v.State, v.Destination.Branch.Name, v.Links.HTML.Href)
	return nil
}

func (r *Request) tokenURL() string {
	o, _ := gitx.Run(r.tokenRepo(), "remote", "get-url", "origin")
	return o
}

// CreateBody is the Bitbucket create payload.
func CreateBody(title, source, dest, desc string) map[string]any {
	br := func(n string) map[string]any { return map[string]any{"branch": map[string]string{"name": n}} }
	return map[string]any{"title": title, "source": br(source), "destination": br(dest), "close_source_branch": true, "description": desc}
}
