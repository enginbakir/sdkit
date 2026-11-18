package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Question is what the agent asked, in the shape of the host's own question tool so the skills map one to one:
// `{"questions":[{"question","header","options":[{"label","description"}],"multiSelect"}]}`.
type Question struct {
	ID        int             `json:"id"`
	Questions json.RawMessage `json:"questions"`
}

// Broker holds the questions the agent has asked and not yet had answered. `sdkit ui ask` blocks in Ask until the person answers
// in the browser (Answer); that block is what keeps the headless session waiting, with no support from the host.
type Broker struct {
	mu      sync.Mutex
	next    int
	pending map[int]*pending
	onAsk   func(Question) // told when a question arrives, so the browser can show it
}

type pending struct {
	q      Question
	answer chan json.RawMessage
}

// NewBroker returns an empty broker; onAsk may be nil.
func NewBroker(onAsk func(Question)) *Broker {
	return &Broker{pending: map[int]*pending{}, onAsk: onAsk}
}

// Ask registers the questions and waits for the answer or for ctx to end. A question that is not a valid questions object is
// refused at once, so the agent sees the mistake instead of waiting on a form nobody can fill in.
func (b *Broker) Ask(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	questions, err := NormalizeQuestions(raw)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.next++
	p := &pending{q: Question{ID: b.next, Questions: questions}, answer: make(chan json.RawMessage, 1)}
	b.pending[p.q.ID] = p
	b.mu.Unlock()
	if b.onAsk != nil {
		b.onAsk(p.q)
	}
	defer func() {
		b.mu.Lock()
		delete(b.pending, p.q.ID)
		b.mu.Unlock()
	}()
	select {
	case a := <-p.answer:
		return a, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Answer delivers the person's answer to the waiting Ask. An unknown id (already answered, or the agent gave up) is an error
// the browser can show.
func (b *Broker) Answer(id int, answer json.RawMessage) error {
	b.mu.Lock()
	p, ok := b.pending[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("question %d is not waiting for an answer", id)
	}
	select {
	case p.answer <- answer:
		return nil
	default:
		return fmt.Errorf("question %d was already answered", id)
	}
}

// Pending lists the questions still waiting, oldest first.
func (b *Broker) Pending() []Question {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Question, 0, len(b.pending))
	for id := 1; id <= b.next; id++ {
		if p, ok := b.pending[id]; ok {
			out = append(out, p.q)
		}
	}
	return out
}
