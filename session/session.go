package session

import (
	"context"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// Session manages conversation message history.
type Session interface {
	ID() string
	Metadata() map[string]any
	State() map[string]any
	SetState(key string, value any)
	Append(ctx context.Context, msgs ...*model.Message) error
	// AppendUser merges parts into the last message when it is already a
	// RoleUser message, otherwise appends a new RoleUser message. A trailing
	// RoleTool message is not a merge target, so tool results and subsequent
	// user input remain distinct messages.
	AppendUser(ctx context.Context, parts ...content.Part) error
	Messages(ctx context.Context) ([]*model.Message, error)
}
