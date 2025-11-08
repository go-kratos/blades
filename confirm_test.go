package blades

import (
	"context"
	"testing"

	"github.com/go-kratos/blades/stream"
)

func TestConfirmMiddleware_Run(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		confirm  ConfirmFunc
		wantErr  error
		wantText string
	}{
		{
			name: "allowed",
			confirm: func(context.Context, *Prompt) (bool, error) {
				return true, nil
			},
			wantErr:  nil,
			wantText: "OK",
		},
		{
			name: "denied",
			confirm: func(context.Context, *Prompt) (bool, error) {
				return false, nil
			},
			wantErr: ErrConfirmDenied,
		},
		{
			name: "error",
			confirm: func(context.Context, *Prompt) (bool, error) {
				return false, ErrMissingFinalResponse
			},
			wantErr: ErrMissingFinalResponse,
		},
	}

	next := &HandleFunc{
		Handle: func(ctx context.Context, p *Prompt, _ ...ModelOption) (*Message, error) {
			return AssistantMessage("OK"), nil
		},
		HandleStream: nil,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := Confirm(tt.confirm)
			h := mw(next)
			got, err := h.Run(context.Background(), NewPrompt(UserMessage("test")))
			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Text() != tt.wantText {
				t.Fatalf("unexpected text: want %q, got %q", tt.wantText, got.Text())
			}
		})
	}
}

func TestConfirmMiddleware_RunStream(t *testing.T) {
	t.Parallel()

	next := &HandleFunc{
		Handle: nil,
		HandleStream: func(ctx context.Context, p *Prompt, _ ...ModelOption) stream.Streamable[*Message] {
			return stream.Just(AssistantMessage("STREAM-OK"))
		},
	}

	t.Run("denied", func(t *testing.T) {
		mw := Confirm(func(context.Context, *Prompt) (bool, error) { return false, nil })
		h := mw(next)
		stream := h.RunStream(context.Background(), NewPrompt(UserMessage("test")))
		for _, err := range stream {
			if err == nil || err.Error() != ErrConfirmDenied.Error() {
				t.Fatalf("expected ErrConfirmationDenied, got %v", err)
			}
		}
	})

	t.Run("allowed", func(t *testing.T) {
		mw := Confirm(func(context.Context, *Prompt) (bool, error) { return true, nil })
		h := mw(next)
		stream := h.RunStream(context.Background(), NewPrompt(UserMessage("test")))
		for msg, err := range stream {
			if err != nil {
				t.Fatalf("unexpected current error: %v", err)
			}
			if msg.Text() != "STREAM-OK" {
				t.Fatalf("unexpected text: want %q, got %q", "STREAM-OK", msg.Text())
			}
		}
	})
}
