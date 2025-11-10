package blades

import (
	"context"
	"testing"
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

	next := HandleFunc(func(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
		return func(yield func(*Message, error) bool) {
			yield(AssistantMessage("OK"), nil)
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := Confirm(tt.confirm)
			h := mw(next)
			for got, err := range h.Handle(context.Background(), &Invocation{
				ID:        "test-invocation-id",
				Message:   UserMessage("test"),
				Session:   NewSession(),
				Resumable: false,
			}) {
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
