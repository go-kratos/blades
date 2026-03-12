package counter_test

import (
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/counter"
)

func TestCharBasedCounter_Empty(t *testing.T) {
	c := counter.NewCharBasedCounter()
	if got := c.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}
}

func TestCharBasedCounter_TextPart(t *testing.T) {
	c := counter.NewCharBasedCounter()
	msg := blades.UserMessage("test") // 4 chars → 1 token + overhead
	got := c.Count(msg)
	if got <= 0 {
		t.Errorf("Count() = %d, want > 0", got)
	}
}

func TestCharBasedCounter_MultipleMessages(t *testing.T) {
	c := counter.NewCharBasedCounter()
	m1 := blades.UserMessage("hello")
	m2 := blades.AssistantMessage("world response here")
	single := c.Count(m1)
	both := c.Count(m1, m2)
	if both <= single {
		t.Errorf("Count(two) = %d should be > Count(one) = %d", both, single)
	}
}

func TestCharBasedCounter_ToolPart(t *testing.T) {
	c := counter.NewCharBasedCounter()
	msg := &blades.Message{
		Role: blades.RoleAssistant,
		Parts: []blades.Part{
			blades.NewToolPart("call-1", "search", `{"query":"go"}`),
		},
	}
	got := c.Count(msg)
	if got <= 0 {
		t.Errorf("Count() = %d, want > 0", got)
	}
}
