package middleware

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-kratos/blades"
)

// TestConversationBuffered verifies that the middleware reads session history
// from context, trims it according to maxMessage, and forwards it to the next handler.
func TestConversationBuffered(t *testing.T) {
	t.Parallel()

	// Helper to create a session with given history messages
	newSessionWithHistory := func(msgs ...*blades.Message) blades.Session {
		s := blades.NewSession()
		// Append history to the session
		for _, m := range msgs {
			_ = s.Append(context.Background(), m)
		}
		return s
	}

	// Prepare some reusable messages
	h1 := blades.UserMessage("h1")
	h2 := blades.AssistantMessage("h2")
	h3 := blades.UserMessage("h3")
	h4 := blades.AssistantMessage("h4")

	tests := []struct {
		name          string
		maxMessage    int
		ctx           context.Context
		initialHist   []*blades.Message
		sessionHist   []*blades.Message
		wantHistTexts []string
	}{
		{
			name:        "no session in context",
			maxMessage:  2,
			ctx:         context.Background(),
			initialHist: []*blades.Message{h1},
			// When no session is present, invocation.History should remain unchanged
			wantHistTexts: []string{"h1"},
		},
		{
			name:       "with session, no trim (maxMessage<=0)",
			maxMessage: 0,
			ctx: func() context.Context {
				s := newSessionWithHistory(h1, h2)
				return blades.NewSessionContext(context.Background(), s)
			}(),
			initialHist:   []*blades.Message{h3}, // initial history remains, session appended
			sessionHist:   []*blades.Message{h1, h2},
			wantHistTexts: []string{"h3", "h1", "h2"},
		},
		{
			name:       "with session, trim to last 1",
			maxMessage: 1,
			ctx: func() context.Context {
				s := newSessionWithHistory(h1, h2, h3)
				return blades.NewSessionContext(context.Background(), s)
			}(),
			initialHist:   []*blades.Message{h4}, // initial history remains, session trimmed and appended
			sessionHist:   []*blades.Message{h1, h2, h3},
			wantHistTexts: []string{"h4", "h3"},
		},
		{
			name:       "with session, equal to limit",
			maxMessage: 3,
			ctx: func() context.Context {
				s := newSessionWithHistory(h1, h2, h3)
				return blades.NewSessionContext(context.Background(), s)
			}(),
			initialHist:   []*blades.Message{h4},
			sessionHist:   []*blades.Message{h1, h2, h3},
			wantHistTexts: []string{"h4", "h1", "h2", "h3"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Capture the history as seen by the next handler
			var seenHistory []*blades.Message
			next := blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
				return func(yield func(*blades.Message, error) bool) {
					// Record the history passed to the next handler
					seenHistory = inv.History
					// Return a simple message to complete the generator
					yield(blades.AssistantMessage("OK"), nil)
				}
			})

			mw := ConversationBuffered(tt.maxMessage)
			handler := mw(next)

			// Build invocation with an initial history to check override behavior
			inv := &blades.Invocation{
				ID:        "inv-id",
				Session:   blades.NewSession(),
				Message:   blades.UserMessage("input"),
				History:   tt.initialHist,
				Resumable: false,
			}

			// Execute the handler
			for _, err := range handler.Handle(tt.ctx, inv) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			// Verify the seen history texts match expectation
			gotTexts := make([]string, 0, len(seenHistory))
			for _, m := range seenHistory {
				gotTexts = append(gotTexts, m.Text())
			}
			if !reflect.DeepEqual(gotTexts, tt.wantHistTexts) {
				t.Fatalf("history mismatch: want %v, got %v", tt.wantHistTexts, gotTexts)
			}
		})
	}
}
