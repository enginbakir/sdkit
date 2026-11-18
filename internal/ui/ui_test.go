package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name, line string
		want       []Event
	}{
		{"banner becomes a step", `{"type":"assistant","message":{"content":[{"type":"text","text":"[sdkit] generate-specs - Step 3/8: Draft - started"}]}}`,
			[]Event{{Kind: KindStep, Skill: "generate-specs", Step: 3, Total: 8, Name: "Draft", State: "started"}}},
		{"banner without a name or colon", `{"type":"user","message":{"content":[{"type":"tool_result","content":"[sdkit] generate-specs - Step 1/8 done\n"}]}}`,
			[]Event{{Kind: KindStep, Skill: "generate-specs", Step: 1, Total: 8, State: "done"}}},
		{"text and banner in one message keep both", `{"type":"assistant","message":{"content":[{"type":"text","text":"Merhaba\n[sdkit] generate-specs - Step 1/8: Validate the input - done"}]}}`,
			[]Event{{Kind: KindText, Text: "Merhaba"}, {Kind: KindStep, Skill: "generate-specs", Step: 1, Total: 8, Name: "Validate the input", State: "done"}}},
		{"tool call is a one-line summary", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"sdkit spec next x"}}]}}`,
			[]Event{{Kind: KindTool, Name: "Bash", Text: "sdkit spec next x"}}},
		{"error result is a failure", `{"type":"result","subtype":"error_max_turns","is_error":true,"result":""}`,
			[]Event{{Kind: KindResult, Failed: true}}},
		{"ok result carries no text, it was shown already", `{"type":"result","subtype":"success","result":"done"}`, []Event{{Kind: KindResult}}},
		{"failure keeps its words", `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`, []Event{{Kind: KindResult, Failed: true, Text: "boom"}}},
		{"banner echoed by a tool still drives the stepper", `{"type":"user","message":{"content":[{"type":"tool_result","content":"[sdkit] g - Step 1/2: Ask - started\n"}]}}`,
			[]Event{{Kind: KindStep, Skill: "g", Step: 1, Total: 2, Name: "Ask", State: "started"}}},
		{"banner in a block-list tool result", `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"[sdkit] g - Step 2/2: Z - done"}]}]}}`,
			[]Event{{Kind: KindStep, Skill: "g", Step: 2, Total: 2, Name: "Z", State: "done"}}},
		{"other tool output is not shown", `{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}`, nil},
		{"garbage gives nothing", `not json`, nil},
		{"unknown content shape gives nothing", `{"type":"assistant","message":{"content":"plain"}}`, nil},
	}
	for _, c := range cases {
		got := ParseLine([]byte(c.line))
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %+v, want %+v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: event %d = %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestBrokerAskWaitsForTheAnswer(t *testing.T) {
	asked := make(chan Question, 1)
	b := NewBroker(func(q Question) { asked <- q })
	got := make(chan string, 1)
	go func() {
		a, err := b.Ask(context.Background(), json.RawMessage(`{"questions":[{"question":"Kapsam?"}]}`))
		if err != nil {
			got <- "err: " + err.Error()
			return
		}
		got <- string(a)
	}()
	q := <-asked
	if len(b.Pending()) != 1 {
		t.Fatalf("pending = %d, want 1", len(b.Pending()))
	}
	if err := b.Answer(q.ID, json.RawMessage(`{"Kapsam?":"A"}`)); err != nil {
		t.Fatal(err)
	}
	if a := <-got; a != `{"Kapsam?":"A"}` {
		t.Errorf("answer = %s", a)
	}
	if err := b.Answer(q.ID, json.RawMessage(`{}`)); err == nil {
		t.Error("a second answer to the same question must be refused")
	}
}

func TestBrokerRefusesWhatCannotBeShown(t *testing.T) {
	b := NewBroker(nil)
	for _, in := range []string{`{}`, `{"questions":[]}`, `nope`} {
		if _, err := b.Ask(context.Background(), json.RawMessage(in)); err == nil {
			t.Errorf("Ask(%s) must fail at once", in)
		}
	}
}

func TestBrokerAskEndsWithItsContext(t *testing.T) {
	b := NewBroker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := b.Ask(ctx, json.RawMessage(`{"questions":[{"question":"q"}]}`)); err == nil {
		t.Fatal("want the context error")
	}
	if len(b.Pending()) != 0 {
		t.Error("an abandoned question must not stay pending")
	}
}

func TestServerGuard(t *testing.T) {
	s := New(Config{Dir: t.TempDir()}, "sekret")
	l, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.Serve(ctx, l)
	base := "http://" + s.addr

	cases := []struct {
		name, host, key, origin string
		want                    int
	}{
		{"right key", "", "sekret", "", 200},
		{"no key", "", "", "", 403},
		{"wrong key", "", "x", "", 403},
		{"rebinding host", "evil.example", "sekret", "", 403},
		{"foreign origin", "", "sekret", "https://evil.example", 403},
		{"own origin", "", "sekret", base, 200},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", base+"/api/questions", nil)
		if c.host != "" {
			req.Host = c.host
		}
		if c.key != "" {
			req.Header.Set("X-Sdkit-Key", c.key)
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.want)
		}
	}
}

func TestAskRoundTrip(t *testing.T) {
	s := New(Config{Dir: t.TempDir()}, "sekret")
	l, _ := s.Listen()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.Serve(ctx, l)
	base := "http://" + s.addr

	out := make(chan string, 1)
	go func() {
		a, err := Ask(ctx, base, "sekret", []byte(`{"questions":[{"question":"Hangisi?","options":[{"label":"A"}]}]}`))
		if err != nil {
			out <- "err: " + err.Error()
			return
		}
		out <- string(a)
	}()
	for i := 0; i < 100 && len(s.broker.Pending()) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	p := s.broker.Pending()
	if len(p) != 1 {
		t.Fatal("the question never reached the panel")
	}
	if err := s.broker.Answer(p[0].ID, json.RawMessage(`{"Hangisi?":"A"}`)); err != nil {
		t.Fatal(err)
	}
	if got := <-out; !strings.Contains(got, `"A"`) {
		t.Errorf("answer = %s", got)
	}
	if _, err := Ask(ctx, "", "", nil); err == nil {
		t.Error("outside a panel session Ask must say so")
	}
}

func TestBuildArgsKeepsAskUserQuestionOff(t *testing.T) {
	a := strings.Join(BuildArgs(RunSpec{Prompt: "p", Tools: []string{"Bash", "Read"}}), " ")
	for _, want := range []string{"--disallowedTools AskUserQuestion", "--allowedTools Bash,Read", "stream-json"} {
		if !strings.Contains(a, want) {
			t.Errorf("args %q lack %q", a, want)
		}
	}
}
