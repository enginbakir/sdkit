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
		Task string `json:"task"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in) != nil || in.Task == "" {
		http.Error(w, `expected {"task":"..."}`, http.StatusBadRequest)
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
	prompt := in.Task
	if s.cfg.Prompt != nil {
		prompt = s.cfg.Prompt(in.Task, askEnv)
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
