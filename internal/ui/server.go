package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/spec"
)

//go:embed web
var webFS embed.FS

// Config is what the server needs from its caller.
type Config struct {
	Dir     string   // the repo
	Command string   // host executable, default `claude`
	Tools   []string // tools a session may use unasked
	Env     []string // passed to every session (tokens)
	Prompt  func(task string, askEnv []string) string
	Getenv  func(string) string // the process environment, handed in by cmd (this package reads none of it)
	HTTP    *http.Client        // for the credential checks; default has a timeout
	// MaxRuns is how many runs may go at once; 0 means 3. Each is a host session of its own, and they share one account's rate limits.
	MaxRuns int
	// SessionExists says whether the host still has this session (it can be resumed); cmd answers from the host's own store.
	SessionExists func(id string) bool
	// BitbucketAPI is the REST base of the Bitbucket checks; default https://api.bitbucket.org/2.0. Tests point it elsewhere.
	BitbucketAPI string
}

// Server is the panel: one repo, several runs at once (one per spec, each in its own worktree), one secret.
type Server struct {
	cfg    Config
	secret string
	addr   string
	broker *Broker

	mu   sync.Mutex
	runs map[string]*runState // the runs going now, by spec id
	logs map[string]*Log      // the latest run's log of each spec, kept after it ends so a page can replay it
	last map[string]string    // the action of that run
}

// NewSecret is the per-start secret that every request must carry.
func NewSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// New builds a server for cfg; Listen sets its address.
func New(cfg Config, secret string) *Server {
	s := &Server{cfg: cfg, secret: secret, runs: map[string]*runState{}, logs: map[string]*Log{}, last: map[string]string{}}
	s.broker = NewBroker(func(q Question) {
		s.logFor(q.Spec).Add(Event{Kind: KindStatus, Name: "question", Text: strconv.Itoa(q.ID)})
	})
	return s
}

// Listen binds 127.0.0.1 on a free port (never another interface) and returns the listener.
func (s *Server) Listen() (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		s.addr = l.Addr().String()
	}
	return l, err
}

// URL is the address to open, secret included.
func (s *Server) URL() string { return "http://" + s.addr + "/?k=" + s.secret }

// Serve runs until ctx ends.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{Handler: s.Handler()}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.Serve(l); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Handler is the whole HTTP surface. Every route needs the secret (header or ?k=), and the Host must be the loopback address the
// server bound to, which stops a page on another site from reaching it through DNS rebinding.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(web)))
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/run", s.run)
	mux.HandleFunc("/api/runs", s.runsList)
	mux.HandleFunc("/api/spec/cleanup", s.specCleanup)
	mux.HandleFunc("/api/stop", s.stop)
	mux.HandleFunc("/api/questions", s.questions)
	mux.HandleFunc("/api/answer", s.answer)
	mux.HandleFunc("/api/ask", s.ask)
	mux.HandleFunc("/api/connection", s.connection)
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/specs", s.specs)
	mux.HandleFunc("/api/prespec", s.prespec)
	mux.HandleFunc("/api/reports", s.reports)
	mux.HandleFunc("/api/report", s.reportText)
	mux.HandleFunc("/api/spec", s.specDetail)
	mux.HandleFunc("/api/spec/reports", s.specReports)
	mux.HandleFunc("/api/spec/file", s.specFile)
	mux.HandleFunc("/api/spec/branch", s.specBranch)
	mux.HandleFunc("/api/spec/change", s.specChange)
	mux.HandleFunc("/api/tasks/commits", s.taskCommits)
	mux.HandleFunc("/api/tasks/parent", s.taskParent)
	mux.HandleFunc("/api/tasks/result", s.taskResult)
	return s.guard(mux)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.addr {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		k := r.Header.Get("X-Sdkit-Key")
		if k == "" {
			k = r.URL.Query().Get("k")
		}
		if subtle.ConstantTimeCompare([]byte(k), []byte(s.secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != s.addr {
				http.Error(w, "bad origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// answer takes {"id": n, "answers": {...}} from the browser.
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      int             `json:"id"`
		Answers json.RawMessage `json:"answers"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in) != nil {
		http.Error(w, `expected {"id":1,"answers":{}}`, http.StatusBadRequest)
		return
	}
	if err := s.broker.Answer(in.ID, in.Answers); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// health is the Health screen: doctor's checks in plain words.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	getenv := s.cfg.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	items := Health(r.Context(), s.cfg.Dir, getenv, s.cfg.Command)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Ready bool   `json:"ready"`
		Items []Item `json:"items"`
	}{Ready(items), items})
}

// connection reads (GET) or saves and then checks (POST) the Jira and Bitbucket settings. A token is never sent back.
func (s *Server) connection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(ReadConnection(s.cfg.Dir))
		return
	}
	var in ConnectionInput
	if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := SaveConnection(s.cfg.Dir, in); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	jt, bt := SavedTokens(s.cfg.Dir)
	hc, api := s.cfg.HTTP, s.cfg.BitbucketAPI
	if hc == nil {
		hc = HTTPClient()
	}
	if api == "" {
		api = "https://api.bitbucket.org/2.0"
	}
	out := struct {
		Saved     bool   `json:"saved"`
		Jira      Check  `json:"jira"`
		Bitbucket *Check `json:"bitbucket,omitempty"`
	}{Saved: true}
	out.Jira = Check{Message: "Jira token'ı henüz girilmedi"}
	if jt != "" {
		out.Jira = CheckJira(r.Context(), hc, in.JiraBaseURL, in.JiraUser, jt)
	}
	if bt != "" {
		c := CheckBitbucket(r.Context(), hc, api, Origin(s.cfg.Dir), ReadConnection(s.cfg.Dir).BitbucketEmail, bt)
		out.Bitbucket = &c
	}
	json.NewEncoder(w).Encode(out)
}

// specs lists every spec with its state: the home screen.
func (s *Server) specs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	items := ListSpecs(s.cfg.Dir)
	if items == nil {
		items = []SpecItem{}
	}
	json.NewEncoder(w).Encode(items)
}

// prespec saves {"id","text","overwrite"} as a new pre-spec.
func (s *Server) prespec(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		Overwrite bool   `json:"overwrite"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, maxPrespec+4096)).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := WritePrespec(s.cfg.Dir, in.ID, in.Text, in.Overwrite); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": in.ID})
}

// checkTaskInput is the server's own look at a tasks run: the SHA is one of the approved commits listed from main, and the
// parent exists in Jira.
func (s *Server) checkTaskInput(ctx context.Context, id, parent, sha string) error {
	ok := false
	for _, c := range spec.ApprovedCommits(s.cfg.Dir, id, 30) {
		ok = ok || c.SHA == sha
	}
	if !ok {
		return errors.New("Bu commit, main'de onaylanmış bir sürüm değil; listeden seçin")
	}
	if p := s.parentCheck(ctx, parent); !p.OK {
		return errors.New(p.Message)
	}
	return nil
}

func (s *Server) parentCheck(ctx context.Context, key string) Parent {
	c := ReadConnection(s.cfg.Dir)
	jt, _ := SavedTokens(s.cfg.Dir)
	hc := s.cfg.HTTP
	if hc == nil {
		hc = HTTPClient()
	}
	return CheckParent(ctx, hc, c.JiraBaseURL, c.JiraUser, jt, key)
}

func (s *Server) taskParent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.parentCheck(r.Context(), strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("key")))))
}

// taskCommits lists the approved versions of a spec on main, newest first: the person picks one, never types a SHA.
func (s *Server) taskCommits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// a merge the Tech Lead made a minute ago must be on the list; best effort, never a prompt, never longer than a few seconds
	fctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	proc.Run(fctx, s.cfg.Dir, []string{"GIT_TERMINAL_PROMPT=0"}, "git", "fetch", "-q", "origin")
	cancel()
	cs := spec.ApprovedCommits(s.cfg.Dir, r.URL.Query().Get("id"), 30)
	if cs == nil {
		cs = []spec.Commit{}
	}
	json.NewEncoder(w).Encode(cs)
}

func (s *Server) taskResult(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ReadTaskResult(s.rootFor(r.URL.Query().Get("id")), r.URL.Query().Get("id"), ReadConnection(s.cfg.Dir).JiraBaseURL))
}

func (s *Server) specRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id] != nil
}

// rootFor is the tree a spec's files are read from and written to: its worktree when it has one.
func (s *Server) rootFor(id string) string { return RootFor(s.cfg.Dir, id) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// specDetail is the page of one spec: the flow bar, what was done, what can be done now, and its files.
func (s *Server) specDetail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	d, err := GetDetail(s.rootFor(id), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	d.Running = s.specRunning(d.ID)
	d.Worktree = s.rootFor(id) != s.cfg.Dir
	d.Resume = ResumeFor(s.cfg.Dir, d.ID, s.rootFor(id), s.cfg.SessionExists)
	for i := range d.Files {
		if d.Running && d.Files[i].Editable {
			d.Files[i].Editable, d.Files[i].Why = false, "Çalışma sürerken dosyalar salt okunur."
		}
	}
	writeJSON(w, http.StatusOK, d)
}

// specFile reads (GET ?id&path) or replaces (PUT {id,path,text}) a file the detail lists as editable.
func (s *Server) specFile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		text, f, err := ReadFile(s.rootFor(id), id, r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"text": text, "editable": f.Editable, "why": f.Why})
		return
	}
	var in struct{ ID, Path, Text string }
	if r.Method != http.MethodPut || json.NewDecoder(io.LimitReader(r.Body, maxFile+8192)).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := WriteFile(s.rootFor(in.ID), in.ID, in.Path, in.Text, s.specRunning(in.ID)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// specBranch moves the repo to the spec's own branch ({"id"}); git refuses with unsaved edits and the person is told.
func (s *Server) specBranch(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in) != nil || !(ValidID(in.ID) || reKitID.MatchString(in.ID)) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.specRunning(in.ID) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Bu spec için bir çalışma sürerken dal değiştirilemez"})
		return
	}
	if err := SwitchBranch(s.rootFor(in.ID), in.ID); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// specChange opens a change on an approved spec ({"id"}); its name comes from who is connected and the time, not from the page.
func (s *Server) specChange(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in) != nil || !(ValidID(in.ID) || reKitID.MatchString(in.ID)) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.specRunning(in.ID) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Bu spec için bir çalışma sürerken değişiklik açılamaz"})
		return
	}
	slug, err := StartChange(s.rootFor(in.ID), in.ID, ReadConnection(s.cfg.Dir).JiraUser, time.Now())
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"slug": slug})
}

// reports lists the kit's session reports, each linked to the spec the panel ran it for (when it did), and the per-spec totals.
func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListReports(s.cfg.Dir))
}

func (s *Server) specReports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, SpecReportsOf(s.cfg.Dir, r.URL.Query().Get("id")))
}

func (s *Server) reportText(w http.ResponseWriter, r *http.Request) {
	t, err := ReportMarkdown(s.cfg.Dir, r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": t})
}
