package memory

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

func TestWindowContextManager_NoLimits(t *testing.T) {
	cm := NewWindowContextManager(WindowConfig{})
	msgs := makeMessages(5)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}
}

func TestWindowContextManager_MaxMessages(t *testing.T) {
	cm := NewWindowContextManager(WindowConfig{MaxMessages: 3})
	msgs := makeMessages(5)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	// Should be the last 3 messages.
	for i, m := range got {
		if m != msgs[2+i] {
			t.Errorf("got[%d] wrong message", i)
		}
	}
}

func TestWindowContextManager_MaxMessages_BelowLimit(t *testing.T) {
	cm := NewWindowContextManager(WindowConfig{MaxMessages: 10})
	msgs := makeMessages(3)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (below limit)", len(got))
	}
}

func TestWindowContextManager_MaxTokens(t *testing.T) {
	cm := NewWindowContextManager(WindowConfig{
		Counter:   NewCharBasedCounter(),
		MaxTokens: 1, // very small budget → keep only the last message
	})
	msgs := makeMessages(5)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 1 {
		t.Errorf("should keep at least 1 message, got 0")
	}
	if len(got) >= len(msgs) {
		t.Errorf("should have truncated, len = %d", len(got))
	}
}

func TestWindowContextManager_Empty(t *testing.T) {
	cm := NewWindowContextManager(WindowConfig{MaxMessages: 5})
	got, err := cm.Prepare(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// helpers shared by window and summary tests

func makeMessages(n int) []*blades.Message {
	msgs := make([]*blades.Message, n)
	for i := range n {
		msgs[i] = blades.UserMessage("message content number " + string(rune('0'+i)))
	}
	return msgs
}
