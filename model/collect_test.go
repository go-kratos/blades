package model

import (
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

	resp, err := Collect(chunkSeq([]*Chunk{
		{Parts: []content.Part{content.Text{Text: "Hel"}}},
		{Parts: []content.Part{content.Text{Text: "lo, "}}},
		{Parts: []content.Part{content.Text{Text: "world"}}},
		{Parts: []content.Part{content.ToolUse{ID: "t1", Name: "bash"}}},
		{StopReason: StopToolUse, Usage: &Usage{OutputTokens: 3}},
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
}
