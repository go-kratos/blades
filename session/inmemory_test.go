package session

import (
	"context"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func textParts(msg *model.Message) []string {
	out := make([]string, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		if t, ok := p.(content.Text); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

func TestAppendUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		initial  []*model.Message
		parts    []content.Part
		wantLen  int
		wantLast *model.Message
	}{
		{
			name:     "empty parts is a no-op",
			initial:  []*model.Message{{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "hi"}}}},
			parts:    nil,
			wantLen:  1,
			wantLast: &model.Message{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "hi"}}},
		},
		{
			name:     "appends to empty session",
			initial:  nil,
			parts:    []content.Part{content.Text{Text: "steer"}},
			wantLen:  1,
			wantLast: &model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "steer"}}},
		},
		{
			name: "merges into trailing user message",
			initial: []*model.Message{
				{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
			},
			parts:    []content.Part{content.Text{Text: " world"}},
			wantLen:  1,
			wantLast: &model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello world"}}},
		},
		{
			name: "does not merge into trailing tool message",
			initial: []*model.Message{
				{Role: model.RoleTool, Parts: []content.Part{content.ToolResult{ID: "call_1"}}},
			},
			parts:    []content.Part{content.Text{Text: "steer"}},
			wantLen:  2,
			wantLast: &model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "steer"}}},
		},
		{
			name: "does not merge into trailing assistant message",
			initial: []*model.Message{
				{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "answer"}}},
			},
			parts:    []content.Part{content.Text{Text: "steer"}},
			wantLen:  2,
			wantLast: &model.Message{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "steer"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess := NewSession(WithMessages(tt.initial...))
			if err := sess.AppendUser(context.Background(), tt.parts...); err != nil {
				t.Fatalf("AppendUser: %v", err)
			}
			msgs, err := sess.Messages(context.Background())
			if err != nil {
				t.Fatalf("Messages: %v", err)
			}
			if len(msgs) != tt.wantLen {
				t.Fatalf("message count = %d, want %d", len(msgs), tt.wantLen)
			}
			last := msgs[len(msgs)-1]
			if last.Role != tt.wantLast.Role {
				t.Fatalf("last role = %q, want %q", last.Role, tt.wantLast.Role)
			}
			gotText := textParts(last)
			wantText := textParts(tt.wantLast)
			if len(gotText) != len(wantText) {
				t.Fatalf("last text parts = %v, want %v", gotText, wantText)
			}
			for i := range gotText {
				if gotText[i] != wantText[i] {
					t.Fatalf("last text parts = %v, want %v", gotText, wantText)
				}
			}
		})
	}
}

func TestAppendUserMergesMultipleSteers(t *testing.T) {
	t.Parallel()
	sess := NewSession()
	ctx := context.Background()
	if err := sess.AppendUser(ctx, content.Text{Text: "first"}); err != nil {
		t.Fatalf("AppendUser: %v", err)
	}
	if err := sess.AppendUser(ctx, content.Text{Text: " second"}); err != nil {
		t.Fatalf("AppendUser: %v", err)
	}
	msgs, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("message count = %d, want 1", len(msgs))
	}
	if got := textParts(msgs[0]); len(got) != 1 || got[0] != "first second" {
		t.Fatalf("merged text = %v, want [\"first second\"]", got)
	}
}

func TestAppendUserDoesNotMutateReturnedCopy(t *testing.T) {
	t.Parallel()
	sess := NewSession(WithMessages(&model.Message{
		Role:  model.RoleUser,
		Parts: []content.Part{content.Text{Text: "hello"}},
	}))
	ctx := context.Background()
	snapshot, err := sess.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if err := sess.AppendUser(ctx, content.Text{Text: " world"}); err != nil {
		t.Fatalf("AppendUser: %v", err)
	}
	// The previously returned snapshot must be unaffected by the merge.
	if got := textParts(snapshot[0]); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("snapshot mutated: %v, want [\"hello\"]", got)
	}
}
