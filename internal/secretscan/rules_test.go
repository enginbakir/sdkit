package secretscan

import (
	"strings"
	"testing"
)

// Samples are built from pieces so this file does not itself look like a leak to `sdkit dev scan`.
func rep(c string, n int) string { return strings.Repeat(c, n) }

func TestRules(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"aws id", "key " + "AKIA" + rep("A", 16), "aws-key-id"},
		{"aws id too short", "AKIA" + rep("A", 15), ""},
		{"aws secret unquoted", "aws_secret_access_key = " + rep("a", 40), "aws-secret-key"},
		{"github server token", "t=" + "ghs" + "_" + rep("a", 30), "github-token"},
		{"slack", "xo" + "xb-" + rep("1", 12), "slack-token"},
		{"openai project key", "sk-" + "proj-" + rep("a", 30), "openai-key"},
		{"plain sk key", "sk-" + rep("a", 32), "api-key"},
		{"sk too short", "sk-" + rep("a", 10), ""},
		{"stripe live", "sk_li" + "ve_" + rep("a", 24), "stripe-key"},
		{"stripe test key is not live", "sk_te" + "st_" + rep("a", 24), ""},
		{"sendgrid", "SG" + "." + rep("a", 22) + "." + rep("b", 43), "sendgrid-key"},
		{"twilio", "SK" + rep("a", 32), "twilio-key"},
		{"npm", "npm" + "_" + rep("a", 36), "npm-token"},
		{"pypi", "pypi-" + "AgEIcHlwaS5vcmc" + rep("a", 60), "pypi-token"},
		{"docker", "dckr" + "_pat_" + rep("a", 30), "docker-token"},
		{"huggingface", "hf" + "_" + rep("a", 34), "huggingface-token"},
		{"azure storage", "AccountKey=" + rep("a", 86) + "==", "azure-storage-key"},
		{"gcp oauth", "ya29" + "." + rep("a", 60), "google-oauth-token"},
		{"private key header", "-----BEGIN RSA " + "PRIVATE KEY-----", "private-key"},
		{"url credential", "postgres://app:" + "hunter22@db.internal", "url-credential"},
		{"connection string", "Server=db;User=a;Password=" + "hunter22;", "connection-string-password"},
		{"password from env is not a secret", "Password=${DB_PASSWORD}", ""},
		{"quoted assignment", `api_key = "` + rep("a", 12) + `"`, "quoted-credential"},
		{"plain prose", "the password is stored in the vault", ""},
	}
	for _, c := range cases {
		got := ""
		lower := strings.ToLower(c.line)
		for _, r := range Rules {
			if r.Match(c.line, lower) {
				got = r.ID
				break
			}
		}
		if got != c.want {
			t.Errorf("%s: %q matched %q, want %q", c.name, c.line, got, c.want)
		}
	}
}

func TestMaskHidesTheSecret(t *testing.T) {
	secret := "AKIA" + rep("Q", 16)
	if got := Mask("k=" + secret); strings.Contains(got, "QQQQ") || !strings.Contains(got, "AKIA***") {
		t.Errorf("mask left the secret visible: %q", got)
	}
}

func TestRuleIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rules {
		if seen[r.ID] {
			t.Errorf("duplicate rule id %s", r.ID)
		}
		seen[r.ID] = true
	}
}
