package graph

import (
	"context"

	kit "github.com/go-kratos/kit/retry"
)

// Retry returns a middleware that retries node handlers with exponential backoff.
func Retry(attempts int, opts ...kit.Option) Middleware {
	r := kit.New(attempts, opts...)
	return func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			var nextState State

			err := r.Do(ctx, func(ctx context.Context) error {
				var err error
				nextState, err = next(ctx, state)
				return err
			})
			if err != nil {
				return nil, err
			}
			return nextState, nil
		}
	}
}
