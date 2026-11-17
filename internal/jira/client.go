package jira

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/audit"
	"github.com/wecii/sdkit/internal/envfile"
	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/redact"
)

// Config is the Jira settings: process environment first, then sdkit.local.env, then sdkit.env.
type Config struct {
	Base, Egress, User, Version, SearchPath, Project, SubtaskType, TaskType, ChangeType, LinkType, Token, SupersededStatus string
	TokenErr                                                                                                               error // deferred so validate-task needs no token
	Timeout                                                                                                                time.Duration
	HTTP                                                                                                                   *http.Client
	AuditLog                                                                                                               string // the audit trail written before every request ("" = none, nothing is sent)
}

var reVar = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Load reads the settings of the repo at root. The token comes only from the
// environment or sdkit.local.env; one found in the committed sdkit.env is refused (exit 3).
func Load(root string, getenv func(string) string) *Config {
	s, p, _ := project.Load(root)
	get := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		if v := s.String(k); v != "" {
			return v
		}
		return def
	}
	c := &Config{Base: get("JIRA_BASE_URL", ""), Egress: get("JIRA_EGRESS", ""), User: get("JIRA_USER", ""), Version: get("JIRA_API_VERSION", "2"),
		SearchPath: get("JIRA_SEARCH_PATH", ""), Project: get("JIRA_PROJECT", ""), SubtaskType: get("JIRA_SUBTASK_TYPE", "Sub-task"), TaskType: get("JIRA_TASK_TYPE", "Task"),
		ChangeType: get("JIRA_CHANGE_TYPE", "Task"), LinkType: get("JIRA_LINK_TYPE", "Blocks"), SupersededStatus: get("JIRA_SUPERSEDED_STATUS", ""), HTTP: http.DefaultClient, AuditLog: audit.Path(p.Dir())}
	secs, _ := strconv.Atoi(get("JIRA_TIMEOUT_S", "60"))
	c.Timeout = time.Duration(max(secs, 1)) * time.Second
	tv := get("JIRA_TOKEN_ENV", "JIRA_API_TOKEN")
	if !reVar.MatchString(tv) {
		c.TokenErr = fail(2, "JIRA_TOKEN_ENV must be a variable NAME such as JIRA_API_TOKEN, not the token itself: put the token in sdkit.local.env as JIRA_API_TOKEN=... (or export it)")
		return c
	}
	shared, local := envfile.New(), envfile.New()
	shared.Quiet, local.Quiet = true, true
	_ = shared.LoadFile(filepath.Join(p.Dir(), p.EnvFile))
	_ = local.LoadFile(filepath.Join(p.Dir(), "sdkit.local.env"))
	if _, ok := shared.Raw(tv); ok {
		c.TokenErr = fail(3, "%s is set in the shared sdkit.env: a token must never be committed. Remove it there (rotate the token) and keep it only in sdkit.local.env", tv)
		return c
	}
	if c.Token = getenv(tv); c.Token == "" {
		if v, ok := local.Raw(tv); ok && v != "" {
			lf := filepath.Join(p.Dir(), "sdkit.local.env")
			if _, err := gitx.Top(root); err == nil && !gitx.IsIgnored(root, lf) {
				c.TokenErr = fail(3, "sdkit.local.env holds the token but is not git-ignored: add it to .gitignore first")
				return c
			}
			c.Token, _ = local.Lookup(tv)
		}
	}
	if c.Token == "" {
		c.TokenErr = fail(3, "jira adapter: no token: the variable named by JIRA_TOKEN_ENV (%s) is empty", tv)
	}
	return c
}

var reKey = regexp.MustCompile(`^[A-Z][A-Za-z0-9_-]*-[0-9]+$`)

// do sends one request and returns the 2xx body. Nothing is sent unless JIRA_EGRESS=allow.
func (c *Config) do(method, path string, body any) ([]byte, error) {
	if c.Egress != "allow" {
		return nil, fail(3, "jira adapter: JIRA_EGRESS=allow is not set: task text would leave this machine, so nothing was sent")
	}
	if !strings.HasPrefix(c.Base, "http://") && !strings.HasPrefix(c.Base, "https://") {
		return nil, fail(2, "jira adapter: JIRA_BASE_URL is not set (http[s]://host)")
	}
	if c.TokenErr != nil {
		return nil, c.TokenErr
	}
	var rd io.Reader
	bs := ""
	if body != nil {
		bs = JSON(body, "")
		rd = strings.NewReader(bs)
	}
	// the audit record comes first: no record, no egress (metadata only: host, verb; never the content)
	hp := strings.TrimRight(strings.SplitN(c.Base, "://", 2)[1], "/")
	hp, _, _ = strings.Cut(hp, "/")
	if c.AuditLog == "" || audit.Log(c.AuditLog, "egress", []string{"adapter=jira", "host=" + hp, "verb=" + verb(method, path), "bytes=" + strconv.Itoa(len(bs))}) != nil {
		return nil, fail(3, "jira adapter: could not write the audit record, so nothing was sent")
	}
	req, _ := http.NewRequest(method, strings.TrimRight(c.Base, "/")+"/rest/api/"+c.Version+"/"+path, rd)
	if c.User != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.User+":"+c.Token)))
	} else {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	cl := *c.HTTP
	cl.Timeout = c.Timeout
	resp, err := cl.Do(req)
	if err != nil {
		return nil, c.redact(fail(5, "jira adapter: request failed: %v", err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode/100 == 2:
		return bytes.TrimSpace(b), nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, fail(3, "jira adapter: HTTP %d (auth refused)", resp.StatusCode)
	case resp.StatusCode == 404:
		return nil, fail(5, "jira adapter: HTTP 404 (not found)")
	}
	return nil, c.redact(fail(5, "jira adapter: HTTP %d: %.300s", resp.StatusCode, b))
}

func (c *Config) redact(e *Err) *Err {
	if c.Token != "" {
		e.Msg = strings.ReplaceAll(e.Msg, c.Token, "***")
	}
	e.Msg = redact.Text(e.Msg)
	return e
}

func keyed(k string) error {
	if !reKey.MatchString(k) {
		return fail(2, "jira adapter: bad issue key")
	}
	return nil
}

func (c *Config) obj(method, path string, body any) (map[string]any, error) {
	b, err := c.do(method, path, body)
	m := map[string]any{}
	if err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	return m, err
}

// Get is the issue as the REST body.
func (c *Config) Get(key string) (map[string]any, error) {
	if err := keyed(key); err != nil {
		return nil, err
	}
	return c.obj("GET", "issue/"+key+"?fields=summary,description,status,parent,labels,issuetype", nil)
}

// Search runs a JQL query (id and labels only).
func (c *Config) Search(jql string) (map[string]any, error) {
	p := c.SearchPath
	if p == "" {
		p = "search"
		if strings.Contains(c.Base, ".atlassian.net") {
			p = "search/jql"
		}
	}
	return c.obj("POST", p, map[string]any{"jql": jql, "fields": []string{"summary", "labels", "status"}, "maxResults": 10})
}

// Create makes an issue and returns its key.
func (c *Config) Create(fields map[string]any) (string, error) {
	r, err := c.obj("POST", "issue", map[string]any{"fields": fields})
	if err != nil {
		return "", err
	}
	k, _ := r["key"].(string)
	if k == "" {
		return "", fail(5, "create returned no issue key")
	}
	return k, nil
}

// Update sets summary and description of an issue.
func (c *Config) Update(key string, fields map[string]any) error {
	if err := keyed(key); err != nil {
		return err
	}
	_, err := c.do("PUT", "issue/"+key, map[string]any{"fields": fields})
	return err
}

// Comment adds a comment (plain text, or one ADF paragraph on API v3).
func (c *Config) Comment(key, text string) error {
	if err := keyed(key); err != nil {
		return err
	}
	var body any = text
	if c.Version == "3" {
		body = map[string]any{"version": 1, "type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}}
	}
	_, err := c.do("POST", "issue/"+key+"/comment", map[string]any{"body": body})
	return err
}

// Transition moves an issue to the status named (a transition name, target status name or id; case-insensitive).
func (c *Config) Transition(key, status string) error {
	r, err := c.obj("GET", "issue/"+key+"/transitions", nil)
	if err == nil {
		err = keyed(key)
	}
	if err != nil {
		return err
	}
	ts, _ := r["transitions"].([]any)
	for _, x := range ts {
		t, _ := x.(map[string]any)
		n, _ := t["name"].(string)
		to, _ := dig(t, "to", "name").(string)
		if id, _ := t["id"].(string); strings.EqualFold(n, status) || strings.EqualFold(to, status) || id == status {
			_, err = c.do("POST", "issue/"+key+"/transitions", map[string]any{"transition": map[string]any{"id": id}})
			return err
		}
	}
	return fail(4, "%s: no transition to '%s' from its current status", key, status)
}

// Link creates an issue link (type, inward and outward keys).
func (c *Config) Link(typ, inward, outward string) error {
	_, err := c.do("POST", "issueLink", map[string]any{"type": map[string]any{"name": typ}, "inwardIssue": map[string]any{"key": inward}, "outwardIssue": map[string]any{"key": outward}})
	return err
}

// verb names a request as the 1.x adapter did (get search create update comment transitions transition link) for the audit record.
func verb(method, path string) string {
	switch {
	case path == "issueLink":
		return "link"
	case path == "issue":
		return "create"
	case strings.HasSuffix(path, "/comment"):
		return "comment"
	case strings.HasSuffix(path, "/transitions"):
		if method == "POST" {
			return "transition"
		}
		return "transitions"
	case method == "PUT":
		return "update"
	case method == "GET":
		return "get"
	}
	return "search"
}
