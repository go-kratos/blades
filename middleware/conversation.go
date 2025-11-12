package middleware

import (
	"context"

	"github.com/go-kratos/blades"
)

// ConversationBuffered is a middleware that manages conversation history within a session.
// It appends the session's message history to the invocation's history before processing.
// The maxMessage parameter limits the number of messages retained from the session history.
func ConversationBuffered(maxMessage int) blades.Middleware {
	// trimMessage trims the message slice to the maximum allowed messages
	trimMessage := func(messages []*blades.Message) []*blades.Message {
		if maxMessage <= 0 || len(messages) <= maxMessage {
			return messages
		}
		return messages[len(messages)-maxMessage:]
	}
	// Return the conversation middleware
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
			session, ok := blades.FromSessionContext(ctx)
			if ok {
				// Append the session history to the invocation history
				invocation.History = append(invocation.History, trimMessage(session.History())...)
			}
			return next.Handle(ctx, invocation)
		})
	}
}
