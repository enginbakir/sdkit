package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
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
	// BitbucketAPI is the REST base of the Bitbucket checks; default https://api.bitbucket.org/2.0. Tests point it elsewhere.
	BitbucketAPI string
}

// Server is the panel: one repo, one run at a time, one secret.
type Server struct {
	cfg    Config
	secret string
	addr   string
	broker *Broker
	log    *Log

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewSecret is the per-start secret that every request must carry.
func NewSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// New builds a server for cfg; Listen sets its address.
func New(cfg Config, secret string) *Server {
	s := &Server{cfg: cfg, secret: secret, log: NewLog()}
	s.broker = NewBroker(func(q Question) {
		s.log.Add(Event{Kind: KindStatus, Name: "question", Text: strconv.Itoa(q.ID)})
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
	mux.HandleFunc("/api/stop", s.stop)
	mux.HandleFunc("/api/questions", s.questions)
	mux.HandleFunc("/api/answer", s.answer)
	mux.HandleFunc("/api/ask", s.ask)
	mux.HandleFunc("/api/connection", s.connection)
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/specs", s.specs)
	mux.HandleFunc("/api/prespec", s.prespec)
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

// events streams the run log as server-sent events; ?from=<seq> resumes after a reconnect.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	past, live, cancel := s.log.Since(from)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(e Event) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b)
		fl.Flush()
	}
	for _, e := range past {
		send(e)
	}
	last := from + len(past)
	for {
		select {
		case e := <-live:
			if e.Seq > last {
				last = e.Seq
				send(e)
			}
		case <-r.Context().Done():
			return
		}
	}
}

// run starts a session for {"task": "..."}; one at a time.
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in) != nil {
		http.Error(w, `expected {"action":"generate-specs","id":"..."}`, http.StatusBadRequest)
		return
	}
	task, err := Task(in.Action, in.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		http.Error(w, "a run is already going", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	// the session reaches the ask bridge through these two variables; `sdkit ui ask` reads them
	askEnv := []string{"SDKIT_UI_URL=http://" + s.addr, "SDKIT_UI_KEY=" + s.secret}
	prompt := task
	if s.cfg.Prompt != nil {
		prompt = s.cfg.Prompt(task, askEnv)
	}
	spec := RunSpec{Dir: s.cfg.Dir, Prompt: prompt, Env: append(append([]string{}, s.cfg.Env...), askEnv...), Command: s.cfg.Command, Tools: s.cfg.Tools}
	go func() {
		Run(ctx, spec, s.log)
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) questions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.broker.Pending())
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

// ask is what `sdkit ui ask` calls from inside the session: it blocks until the person answers, then returns the answers.
func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.Method != http.MethodPost || err != nil {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	a, err := s.broker.Ask(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(a)
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
