package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
)

func TestSummaryContextManager_BelowBudget(t *testing.T) {
	summarizer := &mockSummarizer{}
	cm := NewSummaryContextManager(SummaryConfig{
		MaxTokens:  1_000_000,
		Summarizer: summarizer,
		KeepRecent: 5,
	})
	msgs := makeMessages(3)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (no compression expected)", len(got))
	}
	if summarizer.calls != 0 {
		t.Errorf("summarizer called %d times, want 0", summarizer.calls)
	}
}

func TestSummaryContextManager_CompressesOldMessages(t *testing.T) {
	summarizer := &mockSummarizer{}
	cm := NewSummaryContextManager(SummaryConfig{
		MaxTokens:  10,
		Summarizer: summarizer,
		KeepRecent: 2,
		BatchSize:  3,
		Counter:    NewCharBasedCounter(),
	})
	msgs := makeMessages(8)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) >= len(msgs) {
		t.Errorf("expected compression, got len %d >= original %d", len(got), len(msgs))
	}
	if summarizer.calls == 0 {
		t.Error("summarizer was not called")
	}
}

func TestSummaryContextManager_SummaryNotRecompressed(t *testing.T) {
	summarizer := &mockSummarizer{}
	cm := NewSummaryContextManager(SummaryConfig{
		MaxTokens:  10,
		Summarizer: summarizer,
		KeepRecent: 1,
		BatchSize:  2,
		Counter:    NewCharBasedCounter(),
	})
	summaryMsg := blades.UserMessage("Previous summary content")
	summaryMsg.Metadata = map[string]any{metaCompressedKey: true}
	msgs := []*blades.Message{summaryMsg, blades.UserMessage("recent 1"), blades.UserMessage("recent 2")}
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range got {
		if m.Metadata[metaCompressedKey] == true {
			found = true
		}
	}
	if !found {
		t.Error("summary message was lost after Prepare")
	}
}

func TestSummaryContextManager_ZeroMaxTokens_NoOp(t *testing.T) {
	summarizer := &mockSummarizer{}
	cm := NewSummaryContextManager(SummaryConfig{
		MaxTokens:  0,
		Summarizer: summarizer,
	})
	msgs := makeMessages(20)
	got, err := cm.Prepare(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Errorf("len = %d, want 20 (MaxTokens=0 is no-op)", len(got))
	}
}

// mockSummarizer is a ModelProvider that returns a canned summary.
type mockSummarizer struct {
	calls int
}

func (m *mockSummarizer) Name() string { return "mock-summarizer" }

func (m *mockSummarizer) Generate(_ context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	m.calls++
	var sb strings.Builder
	sb.WriteString("Summary of ")
	for _, msg := range req.Messages {
		sb.WriteString(msg.Text())
	}
	return &blades.ModelResponse{Message: blades.AssistantMessage(sb.String())}, nil
}

func (m *mockSummarizer) NewStreaming(_ context.Context, _ *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {}
}
