package window_test

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/window"
	"github.com/go-kratos/blades/internal/counter"
)

func TestCompressor_NoLimits(t *testing.T) {
	c := window.NewCompressor()
	msgs := makeMessages(5)
	got, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}
}

func TestCompressor_MaxMessages(t *testing.T) {
	c := window.NewCompressor(window.WithMaxMessages(3))
	msgs := makeMessages(5)
	got, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	for i, m := range got {
		if m != msgs[2+i] {
			t.Errorf("got[%d] wrong message", i)
		}
	}
}

func TestCompressor_MaxMessages_BelowLimit(t *testing.T) {
	c := window.NewCompressor(window.WithMaxMessages(10))
	msgs := makeMessages(3)
	got, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (below limit)", len(got))
	}
}

func TestCompressor_MaxTokens(t *testing.T) {
	c := window.NewCompressor(
		window.WithTokenCounter(counter.NewCharBasedCounter()),
		window.WithMaxTokens(1),
	)
	msgs := makeMessages(5)
	got, err := c.Compress(context.Background(), msgs)
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

func TestCompressor_Empty(t *testing.T) {
	c := window.NewCompressor(window.WithMaxMessages(5))
	got, err := c.Compress(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestCompressor_DefaultMaxMessages(t *testing.T) {
	c := window.NewCompressor()
	msgs := makeMessages(150)
	got, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Errorf("len = %d, want 100 (default MaxMessages)", len(got))
	}
}

func TestCompressor_MaxMessagesZero_NoLimit(t *testing.T) {
	c := window.NewCompressor(window.WithMaxMessages(0))
	msgs := makeMessages(150)
	got, err := c.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 {
		t.Errorf("len = %d, want 150 (MaxMessages=0 disables limit)", len(got))
	}
}

func makeMessages(n int) []*blades.Message {
	msgs := make([]*blades.Message, n)
	for i := 0; i < n; i++ {
		msgs[i] = blades.UserMessage("message content number " + string(rune('0'+i)))
	}
	return msgs
}
