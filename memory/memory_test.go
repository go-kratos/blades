package memory

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

func TestInMemory_PerConversationLimit(t *testing.T) {
	mem := NewInMemory(3)
	ctx := context.Background()

	mk := func(s string) *blades.Message { return blades.UserMessage(s) }

	// convA: add 4 messages, expect last 3 remain
	_ = mem.AddMessages(ctx, "A", []*blades.Message{mk("a1")})
	_ = mem.AddMessages(ctx, "A", []*blades.Message{mk("a2"), mk("a3")})
	_ = mem.AddMessages(ctx, "A", []*blades.Message{mk("a4")})

	msgsA, _ := mem.ListMessages(ctx, "A")
	if len(msgsA) != 3 {
		t.Fatalf("convA expected 3, got %d", len(msgsA))
	}
	if msgsA[0].Text() != "a2" || msgsA[1].Text() != "a3" || msgsA[2].Text() != "a4" {
		t.Fatalf("convA unexpected order: %q, %q, %q", msgsA[0].Text(), msgsA[1].Text(), msgsA[2].Text())
	}

	// convB: independent limit
	_ = mem.AddMessages(ctx, "B", []*blades.Message{mk("b1"), mk("b2"), mk("b3")})
	msgsB, _ := mem.ListMessages(ctx, "B")
	if len(msgsB) != 3 {
		t.Fatalf("convB expected 3, got %d", len(msgsB))
	}

	// ensure convA unchanged by operations on convB
	msgsA2, _ := mem.ListMessages(ctx, "A")
	if len(msgsA2) != 3 || msgsA2[2].Text() != "a4" {
		t.Fatalf("convA should remain intact; got len=%d last=%q", len(msgsA2), msgsA2[len(msgsA2)-1].Text())
	}
}

func TestInMemory_Unlimited(t *testing.T) {
	mem := NewInMemory(0) // unlimited
	ctx := context.Background()
	mk := func(s string) *blades.Message { return blades.UserMessage(s) }
	_ = mem.AddMessages(ctx, "A", []*blades.Message{mk("1"), mk("2"), mk("3"), mk("4")})
	msgs, _ := mem.ListMessages(ctx, "A")
	if len(msgs) != 4 {
		t.Fatalf("expected 4, got %d", len(msgs))
	}
}
