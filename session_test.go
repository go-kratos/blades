package blades

import (
	"context"
	"testing"
)

func TestSession_SetState(t *testing.T) {
	s := NewSession()
	s.SetState("key", "value")
	if s.State()["key"] != "value" {
		t.Errorf("state[key] = %v, want 'value'", s.State()["key"])
	}
}

func TestSession_History(t *testing.T) {
	s := NewSession()
	for i := 0; i < 3; i++ {
		s.Append(context.Background(), UserMessage("msg"))
	}
	if len(s.History()) != 3 {
		t.Errorf("history len = %d, want 3", len(s.History()))
	}
}

func TestSession_Context_NoCompressor(t *testing.T) {
	s := NewSession()
	msgs := []*Message{UserMessage("a"), UserMessage("b")}
	got, err := s.Context(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil when no compressor configured, got %v", got)
	}
}

func TestSession_Context_WithCompressor(t *testing.T) {
	// A compressor that always returns only the last message.
	limiter := &limitCompressor{max: 1}
	s := NewSession(WithCompressor(limiter))
	msgs := []*Message{UserMessage("a"), UserMessage("b"), UserMessage("c")}
	got, err := s.Context(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (compressed)", len(got))
	}
	if got[0].Text() != "c" {
		t.Errorf("text = %q, want %q", got[0].Text(), "c")
	}
}

// limitCompressor is a test Compressor that keeps only the last max messages.
type limitCompressor struct{ max int }

func (l *limitCompressor) Compress(_ context.Context, messages []*Message) ([]*Message, error) {
	if len(messages) <= l.max {
		return messages, nil
	}
	return messages[len(messages)-l.max:], nil
}
