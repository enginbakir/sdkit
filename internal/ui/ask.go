package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Ask is the client of the ask bridge: it posts the questions to the panel that started this session and waits, as long as it
// takes, for the person's answer. base and key come from the session's environment (SDKIT_UI_URL, SDKIT_UI_KEY).
func Ask(ctx context.Context, base, key string, questions []byte) ([]byte, error) {
	if base == "" || key == "" {
		return nil, errors.New("not inside an `sdkit ui` session (SDKIT_UI_URL / SDKIT_UI_KEY are not set); ask the user in the host instead")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/ask", bytes.NewReader(questions))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Sdkit-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req) // no timeout: a person answers when they answer
	if err != nil {
		return nil, fmt.Errorf("the panel is not reachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the panel refused the question: %s", strings.TrimSpace(string(body)))
	}
	var check json.RawMessage
	if json.Unmarshal(body, &check) != nil {
		return nil, errors.New("the panel sent an answer that is not JSON")
	}
	return body, nil
}
