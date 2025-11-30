package graph

import (
	"context"

	"github.com/go-kratos/kit/retry"
)

// Retry returns a middleware that retries node handlers with exponential backoff.
//
// Parameters:
//
//	attempts: The total number of attempts to execute the handler, including the initial attempt.
//	          For example, attempts=3 means up to 3 tries (1 initial + 2 retries).
//	opts:     Optional configuration for retry behavior. See retry.Option (from github.com/go-kratos/kit/retry) for details.
//
// Behavior:
//   - The same `state` value is passed to the handler on each attempt. Handlers must not mutate `state`.
//   - If all attempts are exhausted and the handler continues to return an error, the last error is returned and no further retries are performed.
//   - Retry behavior (e.g., backoff, which errors are retryable) can be customized via retry.Option.
//
// Example usage:
//
//	// Retry up to 5 times with exponential backoff, only on specific errors.
//	mw := Retry(5,
//	    retry.WithBackoff(retry.NewExponentialBackoff()),
//	    retry.WithRetryable(func(err error) bool {
//	        return errors.Is(err, ErrTemporary)
//	    }),
//	)
func Retry(attempts int, opts ...retry.Option) Middleware {
	r := retry.New(attempts, opts...)
	return func(next Handler) Handler {
		return func(ctx context.Context, input State) (State, error) {
			var (
				err    error
				output State
			)
			if err = r.Do(ctx, func(ctx context.Context) error {
				output, err = next(ctx, input)
				return err
			}); err != nil {
				return nil, err
			}
			return output, nil
		}
	}
}
