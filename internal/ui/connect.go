package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/gitx"
	"github.com/wecii/sdkit/internal/project"
)

// Connection is what the Connect screen shows: the settings it owns, never a token. A token is only ever reported as set or not.
type Connection struct {
	JiraBaseURL       string `json:"jiraBaseUrl"`
	JiraProject       string `json:"jiraProject"`
	JiraUser          string `json:"jiraUser"`
	JiraTokenSet      bool   `json:"jiraTokenSet"`
	BitbucketEmail    string `json:"bitbucketEmail"`
	BitbucketTokenSet bool   `json:"bitbucketTokenSet"`
}

// ConnectionInput is what the person typed. An empty token keeps the saved one, so the form can be saved again without retyping it.
type ConnectionInput struct {
	JiraBaseURL    string `json:"jiraBaseUrl"`
	JiraProject    string `json:"jiraProject"`
	JiraUser       string `json:"jiraUser"`
	JiraToken      string `json:"jiraToken"`
	BitbucketEmail string `json:"bitbucketEmail"`
	BitbucketToken string `json:"bitbucketToken"`
}

// Check is the outcome of trying one set of credentials, in words the person can act on.
type Check struct {
	OK      bool   `json:"ok"`
	Who     string `json:"who,omitempty"`
	Message string `json:"message,omitempty"`
}

// ReadConnection reads the current settings (shared file first, personal file after it, as everywhere in the kit).
func ReadConnection(root string) Connection {
	s, _, _ := project.Load(root)
	get := s.String
	return Connection{
		JiraBaseURL: get("JIRA_BASE_URL"), JiraProject: get("JIRA_PROJECT"), JiraUser: get("JIRA_USER"),
		JiraTokenSet: get("JIRA_API_TOKEN") != "", BitbucketEmail: get("BITBUCKET_EMAIL"), BitbucketTokenSet: get("BITBUCKET_TOKEN") != "",
	}
}

// isLoopback admits http for an address that never leaves this machine (a local Jira stand-in); everything else must be https.
func isLoopback(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }

var (
	reToken   = regexp.MustCompile(`^[A-Za-z0-9_\-=+/.]{8,}$`) // no `$`, quote or space: the settings reader would expand or cut them
	reProject = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,19}$`)
	reEmail   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// Validate refuses input that could not work or could not be stored safely, with a message for the person. Empty tokens are fine
// (they keep the saved ones).
func (in ConnectionInput) Validate() error {
	base, err := url.Parse(strings.TrimSpace(in.JiraBaseURL))
	switch {
	case err != nil || base.Host == "" || (base.Scheme != "https" && !(base.Scheme == "http" && isLoopback(base.Hostname()))):
		return errors.New("Jira adresi https:// ile başlamalı, örneğin https://sirketiniz.atlassian.net")
	case !reProject.MatchString(in.JiraProject):
		return errors.New("Jira proje anahtarı büyük harflerle yazılır, örneğin TFF")
	case !reEmail.MatchString(in.JiraUser):
		return errors.New("Jira için e-posta adresinizi yazın")
	case in.BitbucketEmail != "" && !reEmail.MatchString(in.BitbucketEmail):
		return errors.New("Bitbucket e-posta adresi geçerli görünmüyor")
	case in.JiraToken != "" && !reToken.MatchString(in.JiraToken):
		return errors.New("Jira token'ı geçersiz karakter içeriyor; kopyalarken boşluk veya satır sonu gelmiş olabilir")
	case in.BitbucketToken != "" && !reToken.MatchString(in.BitbucketToken):
		return errors.New("Bitbucket token'ı geçersiz karakter içeriyor; kopyalarken boşluk veya satır sonu gelmiş olabilir")
	}
	return nil
}

// SaveConnection writes the person's settings to the personal file (sdkit.local.env), never to the shared one: the team's file is
// committed, and a token or someone's e-mail must not ride along. It refuses when that file is not git-ignored.
func SaveConnection(root string, in ConnectionInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	p := project.Resolve(root)
	if !p.Found {
		return errors.New("bu klasörde sdkit kurulumu yok; önce kurulumu tamamlayın")
	}
	path := filepath.Join(p.Dir(), "sdkit.local.env")
	if gitx.Tracked(root, path) || !gitx.IsIgnored(root, path) {
		return errors.New("sdkit.local.env git tarafından yok sayılmıyor; token'ın yanlışlıkla paylaşılmaması için kaydedilmedi")
	}
	kv := []envKV{
		{"TRACKER", "jira"}, {"JIRA_BASE_URL", strings.TrimRight(strings.TrimSpace(in.JiraBaseURL), "/")}, {"JIRA_PROJECT", in.JiraProject},
		{"JIRA_USER", in.JiraUser}, {"JIRA_EGRESS", "allow"},
	}
	if in.JiraToken != "" {
		kv = append(kv, envKV{"JIRA_API_TOKEN", in.JiraToken})
	}
	if in.BitbucketEmail != "" {
		kv = append(kv, envKV{"BITBUCKET_EMAIL", in.BitbucketEmail})
	}
	if in.BitbucketToken != "" {
		kv = append(kv, envKV{"BITBUCKET_TOKEN", in.BitbucketToken})
	}
	return setEnvKeys(path, kv)
}

type envKV struct{ Key, Value string }

// setEnvKeys sets each key in an env file: a live `KEY=` line is replaced in place, otherwise the key is appended, and every
// other line (comments included) is kept as it was. The file is written whole through a temp file, mode 0600.
func setEnvKeys(path string, kv []envKV) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(raw) == 0 {
		lines = nil
	}
	for _, e := range kv {
		if strings.ContainsAny(e.Value, "\r\n") {
			return fmt.Errorf("%s: değer satır sonu içeremez", e.Key)
		}
		line, at := e.Key+"="+e.Value, -1
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), e.Key+"=") {
				at = i // the last live one wins, as when the file is read
			}
		}
		if at >= 0 {
			lines[at] = line
		} else {
			lines = append(lines, line)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sdkit.local.env.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// CheckJira tries the credentials against Jira and says what is wrong in plain words. It sends nothing but the check itself.
func CheckJira(ctx context.Context, c *http.Client, base, user, token string) Check {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/rest/api/2/myself", nil)
	if err != nil {
		return Check{Message: "Jira adresi geçersiz"}
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+token)))
	resp, err := c.Do(req)
	if err != nil {
		return Check{Message: "Jira'ya ulaşılamadı; adresi ve internet bağlantısını kontrol edin"}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var v struct {
			DisplayName string `json:"displayName"`
		}
		json.NewDecoder(resp.Body).Decode(&v)
		return Check{OK: true, Who: v.DisplayName}
	case http.StatusUnauthorized, http.StatusForbidden:
		return Check{Message: "Jira bilgileri kabul etmedi; e-posta adresini ve token'ın süresinin dolmadığını kontrol edin"}
	case http.StatusNotFound:
		return Check{Message: "Bu adreste Jira bulunamadı; adresi kontrol edin"}
	}
	return Check{Message: fmt.Sprintf("Jira beklenmedik bir cevap verdi (HTTP %d)", resp.StatusCode)}
}

var reBitbucketRepo = regexp.MustCompile(`bitbucket\.org[:/]([^/\s]+/[^/\s]+?)(?:\.git)?$`)

// CheckBitbucket tries the credentials on the repository the work will be published to: an access token of that repository cannot
// read the account (/user), so asking for the repository is the check that matches what the token is for. ATCTT tokens are
// sent as Bearer, the rest as e-mail:token, as `sdkit pr` does.
func CheckBitbucket(ctx context.Context, c *http.Client, api, origin, email, token string) Check {
	m := reBitbucketRepo.FindStringSubmatch(strings.TrimSpace(origin))
	if m == nil {
		return Check{Message: "Bu klasör bir Bitbucket deposuna bağlı görünmüyor"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(api, "/")+"/repositories/"+m[1], nil)
	if err != nil {
		return Check{Message: "Bitbucket adresi geçersiz"}
	}
	if strings.HasPrefix(token, "ATCTT") {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(email+":"+token)))
	}
	resp, err := c.Do(req)
	if err != nil {
		return Check{Message: "Bitbucket'a ulaşılamadı; internet bağlantısını kontrol edin"}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return Check{OK: true, Who: m[1]}
	case http.StatusUnauthorized, http.StatusForbidden:
		return Check{Message: "Bitbucket bilgileri kabul etmedi; e-posta adresini ve token'ın depoyu okuma ve pull request yazma izni olduğunu kontrol edin"}
	case http.StatusNotFound:
		return Check{Message: "Depo bulunamadı veya bu token ona erişemiyor"}
	}
	return Check{Message: fmt.Sprintf("Bitbucket beklenmedik bir cevap verdi (HTTP %d)", resp.StatusCode)}
}

// HTTPClient is the client the checks use: they must not hang the form.
func HTTPClient() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

// SavedTokens returns the saved tokens so a form saved with empty token fields can still be checked. They stay in this process.
func SavedTokens(root string) (jira, bitbucket string) {
	s, _, _ := project.Load(root)
	return s.String("JIRA_API_TOKEN"), s.String("BITBUCKET_TOKEN")
}

// Origin is the URL of the repository's origin remote, or empty.
func Origin(root string) string {
	o, _ := gitx.Run(root, "remote", "get-url", "origin")
	return strings.TrimSpace(o)
}
