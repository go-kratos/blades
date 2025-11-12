package blades

import "context"

// ConversationBuffered is a middleware that manages conversation history within a session.
// It appends the session's message history to the invocation's history before processing.
// The maxMessage parameter limits the number of messages retained from the session history.
func ConversationBuffered(maxMessage int) Middleware {
	// trimMessage trims the message slice to the maximum allowed messages
	trimMessage := func(messages []*Message) []*Message {
		if maxMessage <= 0 || len(messages) <= maxMessage {
			return messages
		}
		return messages[len(messages)-maxMessage:]
	}
	// Return the conversation middleware
	return func(next Handler) Handler {
		return HandleFunc(func(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
			session, ok := FromSessionContext(ctx)
			if ok {
				history, err := session.History(ctx)
				if err != nil {
					return func(yield func(*Message, error) bool) {
						yield(nil, err)
					}
				}
				// Append the session history to the invocation history
				invocation.History = trimMessage(history)
			}
			return next.Handle(ctx, invocation)
		})
	}
}
