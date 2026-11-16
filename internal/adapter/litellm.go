package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Audit writes one metadata-only record (the hash-chained audit log, P3-07 supplies it). Nil, or an error, means no record:
// and with no record nothing leaves the machine. Redact masks credential-shaped strings in a message (P3-07); nil keeps it.
var (
	Audit  func(event string, kv ...string) error
	Redact func(string) string
)

// Error is a refused or failed external call: Code is the exit code run-role passes on (2 usage, 4 refused, 5 provider).
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// LiteLLM is the optional OpenAI-compatible chat adapter (`--host ext:litellm`), e.g. a LiteLLM proxy. Off unless configured.
// Text only: no tools, no file access, so it refuses a role that writes and the independent roles that must inspect the tree.
// Settings (get reads sdkit.local.env, then the environment; the key is never read from a repo file):
//
//	LITELLM_BASE_URL (required), LITELLM_MODEL (unless model is given), LITELLM_API_KEY_ENV (NAME of the variable that
//	holds the key, default LITELLM_API_KEY; empty key means no Authorization header), LITELLM_EGRESS=allow (required:
//	the prompt leaves this machine), LITELLM_ALLOW_HOSTS (host[:port] list, default localhost and 127.0.0.1), LITELLM_TIMEOUT_S.
//
// It returns {"result": text, "usage": {input_tokens, output_tokens}?} in the shape every ext adapter prints.
func LiteLLM(ctx context.Context, get func(string) string, role, model string, ro bool, prompt string) (out []byte, err *Error) {
	key := ""
	die := func(code int, f string, a ...any) (_ []byte, e *Error) {
		m := "litellm adapter: " + fmt.Sprintf(f, a...)
		if key != "" {
			m = strings.ReplaceAll(m, key, "***")
		}
		if Redact != nil {
			m = Redact(m)
		}
		return nil, &Error{code, m}
	}
	switch strings.TrimSuffix(role, "-deep") {
	case "verifier", "reviewer", "security-auditor":
		return die(4, "refuses %s: a text-only adapter cannot inspect the tree itself, and an independent verdict must not rest on pasted content", role)
	}
	if !ro {
		return die(4, "refuses a role that writes files (text-only adapter)")
	}
	if get("LITELLM_EGRESS") != "allow" {
		return die(4, "LITELLM_EGRESS=allow is not set: the prompt would leave this machine, so nothing was sent")
	}
	base := get("LITELLM_BASE_URL")
	if base == "" {
		return die(4, "LITELLM_BASE_URL is not set")
	}
	if model == "" {
		model = get("LITELLM_MODEL")
	}
	if model == "" {
		return die(4, "no model (--model or LITELLM_MODEL)")
	}
	hp := base
	if i := strings.Index(hp, "://"); i >= 0 {
		hp = hp[i+3:]
	}
	hp, _, _ = strings.Cut(hp, "/")
	allow := strings.Fields(get("LITELLM_ALLOW_HOSTS"))
	if len(allow) == 0 {
		allow = []string{"localhost:4000", "127.0.0.1:4000", "localhost", "127.0.0.1"}
	}
	name, _, _ := strings.Cut(hp, ":")
	ok := false
	for _, a := range allow {
		ok = ok || hp == a || name == a
	}
	if !ok {
		return die(4, "host '%s' is not in LITELLM_ALLOW_HOSTS", hp)
	}
	// the audit record comes first: no record, no egress (metadata only: host, model, role, bytes; never the prompt)
	if Audit == nil || Audit("egress", "adapter=litellm", "host="+hp, "model="+model, "role="+role, "bytes="+strconv.Itoa(len(prompt))) != nil {
		return die(4, "could not write the audit record, so nothing was sent")
	}
	kv := get("LITELLM_API_KEY_ENV")
	if kv == "" {
		kv = "LITELLM_API_KEY"
	}
	key = get(kv)
	body, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}})
	secs, _ := strconv.Atoi(get("LITELLM_TIMEOUT_S"))
	if secs <= 0 {
		secs = 300
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(secs)*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if e != nil {
		return die(2, "cannot build the request")
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return die(5, "request failed: %s", clip(e.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var r struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	var u struct {
		Usage struct {
			P *int `json:"prompt_tokens"`
			C *int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return die(5, "unexpected response shape (no choices[0].message.content)")
	}
	json.Unmarshal(raw, &u)
	if len(r.Error) > 0 && string(r.Error) != "null" && string(r.Error) != "false" {
		msg := string(r.Error)
		var em struct{ Message string }
		if json.Unmarshal(r.Error, &em) == nil && em.Message != "" {
			msg = em.Message
		}
		return die(5, "provider error: %s", clip(msg))
	}
	if len(r.Choices) == 0 || r.Choices[0].Message.Content == nil {
		return die(5, "unexpected response shape (no choices[0].message.content)")
	}
	res := map[string]any{"result": *r.Choices[0].Message.Content}
	if u.Usage.P != nil && u.Usage.C != nil {
		res["usage"] = map[string]int{"input_tokens": *u.Usage.P, "output_tokens": *u.Usage.C}
	}
	out, _ = json.Marshal(res)
	return out, nil
}

func clip(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
