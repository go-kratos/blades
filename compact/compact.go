package compact

import (
	"context"

	"github.com/go-kratos/blades/model"
)

// Compactor transforms a message slice to fit within context budget.
type Compactor interface {
	Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)
}

// CompactorFunc is a function adapter for Compactor.
type CompactorFunc func(ctx context.Context, msgs []*model.Message) ([]*model.Message, error)

func (f CompactorFunc) Compact(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
	return f(ctx, msgs)
}

// Chain composes multiple compactors in sequence.
func Chain(cs ...Compactor) Compactor {
	return CompactorFunc(func(ctx context.Context, msgs []*model.Message) ([]*model.Message, error) {
		var err error
		for _, c := range cs {
			msgs, err = c.Compact(ctx, msgs)
			if err != nil {
				return nil, err
			}
		}
		return msgs, nil
	})
}

// TokenCounter counts tokens for messages.
type TokenCounter interface {
	Count(msgs ...*model.Message) int64
}
