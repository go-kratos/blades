package summary_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/internal/counter"
)

func TestContextManager_BelowBudget(t *testing.T) {
	s := &mockSummarizer{}
	cm := summary.NewContextManager(
		summary.WithSummarizer(s),
		summary.WithMaxTokens(1_000_000),
		summary.WithKeepRecent(5),
	)
	msgs := makeMessages(3)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (no compression expected)", len(got))
	}
	if s.calls != 0 {
		t.Errorf("summarizer called %d times, want 0", s.calls)
	}
}

func TestContextManager_CompressesOldMessages(t *testing.T) {
	s := &mockSummarizer{}
	cm := summary.NewContextManager(
		summary.WithSummarizer(s),
		summary.WithMaxTokens(10),
		summary.WithKeepRecent(2),
		summary.WithBatchSize(3),
		summary.WithTokenCounter(counter.NewCharBasedCounter()),
	)
	msgs := makeMessages(8)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) >= len(msgs) {
		t.Errorf("expected compression, got len %d >= original %d", len(got), len(msgs))
	}
	if s.calls == 0 {
		t.Error("summarizer was not called")
	}
}

func TestContextManager_ZeroMaxTokens_NoOp(t *testing.T) {
	s := &mockSummarizer{}
	cm := summary.NewContextManager(summary.WithSummarizer(s)) // MaxTokens=0 → no-op
	msgs := makeMessages(20)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Errorf("len = %d, want 20 (MaxTokens=0 is no-op)", len(got))
	}
}

func TestContextManager_WithInstruction(t *testing.T) {
	s := &mockSummarizer{}
	customInstrText := "Summarize in Chinese. Output only the summary."
	cm := summary.NewContextManager(
		summary.WithSummarizer(s),
		summary.WithMaxTokens(10),
		summary.WithKeepRecent(2),
		summary.WithBatchSize(3),
		summary.WithTokenCounter(counter.NewCharBasedCounter()),
		summary.WithInstruction(customInstrText),
	)
	msgs := makeMessages(8)
	_, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if s.calls == 0 {
		t.Fatal("summarizer was not called")
	}
	if s.lastReq.Instruction == nil {
		t.Fatal("expected Instruction to be set on ModelRequest, got nil")
	}
	if s.lastReq.Instruction.Text() != customInstrText {
		t.Errorf("Instruction text = %q, want %q", s.lastReq.Instruction.Text(), customInstrText)
	}
}

func makeMessages(n int) []*blades.Message {
	msgs := make([]*blades.Message, n)
	for i := range n {
		msgs[i] = blades.UserMessage("message content number " + string(rune('0'+i)))
	}
	return msgs
}

type mockSummarizer struct {
	calls   int
	lastReq *blades.ModelRequest
}

func (m *mockSummarizer) Name() string { return "mock" }

func (m *mockSummarizer) Generate(_ context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	m.calls++
	m.lastReq = req
	var sb strings.Builder
	for _, msg := range req.Messages {
		sb.WriteString(msg.Text())
	}
	return &blades.ModelResponse{Message: blades.AssistantMessage("Summary: " + sb.String())}, nil
}

func (m *mockSummarizer) NewStreaming(_ context.Context, _ *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {}
}
