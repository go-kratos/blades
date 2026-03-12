package blades

import (
	"context"
	"testing"
)

func TestSession_MaxHistorySize_Evicts(t *testing.T) {
	s := NewSession(WithMaxHistorySize(3))
	for range 5 {
		s.Append(context.Background(), UserMessage("msg"))
	}
	if len(s.History()) != 3 {
		t.Errorf("history len = %d, want 3", len(s.History()))
	}
}

func TestSession_MaxHistorySize_Zero_NoLimit(t *testing.T) {
	s := NewSession()
	for range 10 {
		s.Append(context.Background(), UserMessage("msg"))
	}
	if len(s.History()) != 10 {
		t.Errorf("history len = %d, want 10", len(s.History()))
	}
}

func TestSession_SetState(t *testing.T) {
	s := NewSession()
	s.SetState("key", "value")
	if s.State()["key"] != "value" {
		t.Errorf("state[key] = %v, want 'value'", s.State()["key"])
	}
}
