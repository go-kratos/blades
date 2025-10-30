package session

import (
	"context"

	"github.com/go-kratos/blades"
)

// SessionService is the interface that defines session management operations.
type SessionService interface {
	CreateSession(ctx context.Context) (*blades.Session, error)
	GetSession(ctx context.Context, sessionID string) (*blades.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context) ([]*blades.Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]*blades.Message, error)
	AddMessage(ctx context.Context, session *blades.Session, message *blades.Message) error
}
