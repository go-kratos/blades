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
	history, err := s.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Errorf("history len = %d, want 3", len(history))
	}
}

func TestSession_Context_NoCompressor(t *testing.T) {
	s := NewSession()
	msg := UserMessage("a")
	s.Append(context.Background(), msg)
	got, err := s.Context(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected raw history, got %v", got)
	}
}

func TestSession_Context_WithContextCompressor(t *testing.T) {
	// A context compressor that always returns only the last message.
	limiter := &limitCompressor{max: 1}
	s := NewSession(WithContextCompressor(limiter))
	for _, text := range []string{"a", "b", "c"} {
		s.Append(context.Background(), UserMessage(text))
	}
	got, err := s.Context(context.Background())
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

// limitCompressor is a test ContextCompressor that keeps only the last max messages.
type limitCompressor struct{ max int }

func (l *limitCompressor) Compress(_ context.Context, messages []*Message) ([]*Message, error) {
	if len(messages) <= l.max {
		return messages, nil
	}
	return messages[len(messages)-l.max:], nil
}
