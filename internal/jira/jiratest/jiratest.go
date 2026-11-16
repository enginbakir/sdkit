// Package jiratest is an httptest Jira for tests: issue get, search, create, update, transitions, comment, attachments, links.
package jiratest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Issue is a canned issue. Desc carries the sdkit-task block.
type Issue struct{ Desc, Status, Category, Parent string }

// Server records Calls ("METHOD /path") and Bodies; Auth is the last Authorization header.
// The handler holds the lock; a test locks it too before it reads the records or changes Issues.
type Server struct {
	sync.Mutex
	URL, Auth     string
	Issues        map[string]Issue
	Calls, Bodies []string
}

// New starts a fake Jira (REST v2 or v3) closed with the test.
func New(t testing.TB, issues map[string]Issue) *Server {
	s := &Server{Issues: issues}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.Lock()
		defer s.Unlock()
		s.Auth, s.Calls, s.Bodies = r.Header.Get("Authorization"), append(s.Calls, r.Method+" "+r.URL.Path), append(s.Bodies, string(b))
		_, p, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/rest/api/"), "/") // any API version
		switch k, _, _ := strings.Cut(strings.TrimPrefix(p, "issue/"), "/"); {
		case r.Method == "GET" && strings.HasSuffix(p, "/transitions"):
			fmt.Fprint(w, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress"}},{"id":"21","name":"Blocked","to":{"name":"Blocked"}},{"id":"31","name":"Done","to":{"name":"Done"}}]}`)
		case r.Method == "GET" && strings.HasPrefix(p, "issue/"):
			i, ok := s.Issues[k]
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"key":%q,"fields":{"description":%q,"status":{"name":%q,"statusCategory":{"key":%q}},"parent":{"key":%q}}}`, k, i.Desc, i.Status, i.Category, i.Parent)
		case p == "issue":
			fmt.Fprint(w, `{"key":"SDK-100"}`)
		case strings.HasPrefix(p, "search"):
			fmt.Fprint(w, `{"issues":[]}`)
		default: // update, comment, attachments, transition, link
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(h.Close)
	s.URL = h.URL
	return s
}
