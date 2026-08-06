package model

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestApproxTokenCounterReturnsSegmentBreakdown(t *testing.T) {
	t.Parallel()

	count, err := ApproxTokenCounter{}.CountTokens(context.Background(), &Request{
		System: "system prompt",
		Messages: []*Message{
			{Role: RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
		},
		Tools: []tools.ToolSpec{{Name: "lookup", Description: "Search"}},
	})
	if err != nil {
		t.Fatalf("ApproxTokenCounter.CountTokens() error = %v", err)
	}
	if !count.HasSegments() {
		t.Fatal("ApproxTokenCounter.CountTokens().HasSegments() = false, want true")
	}
	if count.System <= 0 || count.Messages <= 0 || count.Tools <= 0 {
		t.Fatalf("ApproxTokenCounter.CountTokens() = %#v, want positive segment counts", count)
	}
	wantInput := count.System + count.Messages + count.Tools
	if count.Input != wantInput {
		t.Fatalf("Input = %d, want %d", count.Input, wantInput)
	}
}

func TestApproxTokenCounterHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ApproxTokenCounter{}.CountTokens(ctx, &Request{})
	if err != context.Canceled {
		t.Fatalf("ApproxTokenCounter.CountTokens() error = %v, want context.Canceled", err)
	}
}

func TestApproxTokenCounterIncludesResponseFormat(t *testing.T) {
	t.Parallel()

	counter := ApproxTokenCounter{}
	base, err := counter.CountTokens(context.Background(), &Request{})
	if err != nil {
		t.Fatalf("ApproxTokenCounter.CountTokens() error = %v", err)
	}
	withSchema, err := counter.CountTokens(context.Background(), &Request{
		Options: []Option{ResponseFormat{
			Schema: &jsonschema.Schema{
				Type:        "object",
				Description: strings.Repeat("structured output schema ", 20),
			},
			Strict: true,
		}},
	})
	if err != nil {
		t.Fatalf("ApproxTokenCounter.CountTokens() error = %v", err)
	}
	if withSchema.Total() <= base.Total() {
		t.Fatalf("count with response schema = %d, want greater than base count %d", withSchema.Total(), base.Total())
	}
}
