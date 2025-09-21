package blades

import "context"

// Memory is a generic interface for storing and retrieving messages of any type.
type Memory interface {
	AddMessages(context.Context, string, []*Message) error
	ListMessages(context.Context, string) ([]*Message, error)
	Clear(context.Context, string) error
}
