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
