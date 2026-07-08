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
	// AppendUser merges parts into the last message when it is a RoleUser or
	// RoleTool message, otherwise appends a new RoleUser message. Merging into a
	// trailing RoleTool message keeps that message's role, so its tool results and
	// the appended parts stay in one message and provider adapters still emit the
	// appended parts as trailing user content. It preserves provided content part
	// boundaries instead of coalescing adjacent text parts; assistant output
	// coalescing belongs to assistant response assembly.
	AppendUser(ctx context.Context, parts ...content.Part) error
	Messages(ctx context.Context) ([]*model.Message, error)
}
