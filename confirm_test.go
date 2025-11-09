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
			confirm: func(context.Context, *Message) (bool, error) {
				return true, nil
			},
			wantErr:  nil,
			wantText: "OK",
		},
		{
			name: "denied",
			confirm: func(context.Context, *Message) (bool, error) {
				return false, nil
			},
			wantErr: ErrConfirmDenied,
		},
		{
			name: "error",
			confirm: func(context.Context, *Message) (bool, error) {
				return false, ErrNoFinalResponse
			},
			wantErr: ErrNoFinalResponse,
		},
	}

	next := HandleFunc(func(ctx context.Context, invocation *Invocation) Sequence[*Message, error] {
		return stream.Just(AssistantMessage("OK"))
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := Confirm(tt.confirm)
			h := mw(next)
			for got, err := range h.Handle(context.Background(), &Invocation{Message: UserMessage("test")}) {
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
			}
		})
	}
}
