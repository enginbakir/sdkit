package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/wecii/sdkit/internal/proc"
)

// Log is the ordered record of one run, replayed to a browser that connects late and streamed to one that is connected.
type Log struct {
	mu     sync.Mutex
	events []Event
	subs   map[chan Event]struct{}
}

// NewLog returns an empty log.
func NewLog() *Log { return &Log{subs: map[chan Event]struct{}{}} }

// Add stamps and stores the event and hands it to every subscriber. A subscriber that cannot keep up misses live events and
// must re-read with Since; the log never blocks the session.
func (l *Log) Add(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Seq = len(l.events) + 1
	l.events = append(l.events, e)
	for c := range l.subs {
		select {
		case c <- e:
		default:
		}
	}
	return e
}

// Since returns the events after seq, and a channel for the next ones; call the returned cancel when done.
func (l *Log) Since(seq int) ([]Event, <-chan Event, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if seq < 0 || seq > len(l.events) {
		seq = 0
	}
	past := append([]Event(nil), l.events[seq:]...)
	c := make(chan Event, 64)
	l.subs[c] = struct{}{}
	return past, c, func() {
		l.mu.Lock()
		delete(l.subs, c)
		l.mu.Unlock()
	}
}

// RunSpec is what a run needs; the caller (cmd) fills it from the environment, this package reads none of it.
type RunSpec struct {
	Dir     string   // the repo the session works in
	Prompt  string   // the task, in the person's words plus the UI rules
	Env     []string // added to the process environment (the ask bridge address and its secret, tokens)
	Command string   // the host executable, `claude` by default
	Tools   []string // tools the session may use without asking, since nobody is at a terminal
	// OnSession is told the host's session id once, when the host first prints it: the id the kit's reports are filed under.
	OnSession func(id string)
}

// BuildArgs is the host command line of a run. AskUserQuestion is disallowed: there is no terminal to show it on, and the skills
// are told to use `sdkit ui ask` instead.
func BuildArgs(s RunSpec) []string {
	args := []string{
		"-p", s.Prompt,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits",
		"--disallowedTools", "AskUserQuestion",
	}
	if len(s.Tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(s.Tools, ","))
	}
	return args
}

// Run starts the session and writes its events to log until it ends or ctx is cancelled (then the whole process tree is killed).
// It returns the host's exit error, if any, after the closing event is in the log.
func Run(ctx context.Context, s RunSpec, log *Log) error {
	bin := s.Command
	if bin == "" {
		bin = "claude"
	}
	if _, err := proc.LookPath(bin); err != nil {
		log.Add(Event{Kind: KindResult, Failed: true, Text: fmt.Sprintf("%s kurulu değil; önce Claude Code kurulmalı ve giriş yapılmalı", bin)})
		return err
	}
	cmd := exec.Command(bin, BuildArgs(s)...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Stderr = nil
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		log.Add(Event{Kind: KindResult, Failed: true, Text: err.Error()})
		return err
	}
	log.Add(Event{Kind: KindStatus, Text: "started"})
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			proc.KillTree(cmd)
		case <-done:
		}
	}()
	// the host prints a result at the end of every turn (a background subagent finishing starts a new one); the run is over only
	// when the process is, so keep the last result and send it then
	var last *Event
	sid := ""
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20) // one tool result can be a whole file
	for sc.Scan() {
		if sid == "" {
			if sid = sessionID(sc.Bytes()); sid != "" && s.OnSession != nil {
				s.OnSession(sid)
			}
		}
		for _, e := range ParseLine(sc.Bytes()) {
			if e.Kind == KindResult {
				e := e
				last = &e
				continue
			}
			log.Add(e)
		}
	}
	werr := cmd.Wait()
	close(done)
	switch {
	case ctx.Err() != nil:
		log.Add(Event{Kind: KindResult, Failed: true, Text: "Durduruldu."})
	case last == nil:
		log.Add(Event{Kind: KindResult, Failed: true, Text: "Oturum sonuç vermeden kapandı."})
	default:
		log.Add(*last)
	}
	return werr
}

// sessionID is the session_id a stream-json line carries, "" when it has none. The pre-check keeps the common line from being parsed twice.
func sessionID(line []byte) string {
	if !bytes.Contains(line, []byte(`"session_id"`)) {
		return ""
	}
	var m struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &m) != nil || !reSession.MatchString(m.SessionID) {
		return ""
	}
	return m.SessionID
}

var reSession = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
