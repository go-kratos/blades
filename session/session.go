package session

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// Session manages conversation message history.
type Session interface {
	ID() string
	Metadata() map[string]any
	State() map[string]any
	SetState(key string, value any)
	Append(ctx context.Context, msgs ...*model.Message) error
	Messages(ctx context.Context) ([]*model.Message, error)
}
