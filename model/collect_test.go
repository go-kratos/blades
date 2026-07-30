package model

import (
	"encoding/json"
	"iter"
	"testing"

	"github.com/go-kratos/blades/content"
)

func chunkSeq(chunks []*Chunk) iter.Seq2[*Chunk, error] {
	return func(yield func(*Chunk, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func TestCollectCoalescesStreamedText(t *testing.T) {
	t.Parallel()

	rawUsage := json.RawMessage(`{"output_tokens":3,"reasoning_tokens":2}`)
	resp, err := Collect(chunkSeq([]*Chunk{
		{Parts: []content.Part{content.Text{Text: "Hel"}}},
		{Parts: []content.Part{content.Text{Text: "lo, "}}},
		{Parts: []content.Part{content.Text{Text: "world"}}},
		{Parts: []content.Part{content.ToolUse{ID: "t1", Name: "bash"}}},
		{StopReason: StopToolUse, Usage: &Usage{TotalOutputTokens: 3, TotalTokens: 3, Raw: rawUsage}},
	}))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	parts := resp.Message.Parts
	if got, want := len(parts), 2; got != want {
		t.Fatalf("parts len = %d, want %d (%#v)", got, want, parts)
	}
	text, ok := parts[0].(content.Text)
	if !ok {
		t.Fatalf("parts[0] = %T, want content.Text", parts[0])
	}
	if text.Text != "Hello, world" {
		t.Fatalf("coalesced text = %q, want %q", text.Text, "Hello, world")
	}
	if _, ok := parts[1].(content.ToolUse); !ok {
		t.Fatalf("parts[1] = %T, want content.ToolUse", parts[1])
	}
	if resp.StopReason != StopToolUse {
		t.Fatalf("stop reason = %q, want %q", resp.StopReason, StopToolUse)
	}
	if got, want := resp.Usage.TotalOutputTokens, int64(3); got != want {
		t.Fatalf("total output tokens = %d, want %d", got, want)
	}
	if got, want := string(resp.Usage.Raw), string(rawUsage); got != want {
		t.Fatalf("raw usage = %s, want %s", got, want)
	}
}

func TestCollectUsesLastUsageAndLastNonEmptyStopReason(t *testing.T) {
	t.Parallel()

	firstRaw := json.RawMessage(`{"total_tokens":7}`)
	lastRaw := json.RawMessage(`{"total_tokens":13}`)
	resp, err := Collect(chunkSeq([]*Chunk{
		{
			StopReason: StopEnd,
			Usage: &Usage{
				TotalInputTokens:  3,
				TotalOutputTokens: 4,
				TotalTokens:       7,
				Raw:               firstRaw,
			},
		},
		{
			Usage: &Usage{
				InputCachedTokens: 5,
				TotalInputTokens:  8,
				TotalOutputTokens: 5,
				TotalTokens:       13,
				Raw:               lastRaw,
			},
		},
		{StopReason: StopToolUse},
	}))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got, want := resp.StopReason, StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	wantUsage := Usage{
		InputCachedTokens: 5,
		TotalInputTokens:  8,
		TotalOutputTokens: 5,
		TotalTokens:       13,
		Raw:               lastRaw,
	}
	if got := resp.Usage; got.InputCachedTokens != wantUsage.InputCachedTokens ||
		got.TotalInputTokens != wantUsage.TotalInputTokens ||
		got.TotalOutputTokens != wantUsage.TotalOutputTokens ||
		got.TotalTokens != wantUsage.TotalTokens ||
		string(got.Raw) != string(wantUsage.Raw) {
		t.Fatalf("usage = %+v, want %+v", got, wantUsage)
	}
}
