package middleware

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/kit/retry"
)

// Retry returns a middleware that retries handlers with configurable retry behavior.
//
// Parameters:
//
//	attempts: The total number of attempts to execute the handler, including the initial attempt.
//	          For example, attempts=3 means up to 3 tries (1 initial + 2 retries).
//	opts:     Optional configuration for retry behavior. See retry.Option (from github.com/go-kratos/kit/retry) for details.
//
// Behavior:
//   - The same invocation is passed to the handler on each attempt. Handlers must not mutate the invocation.
//   - If all attempts are exhausted and the handler continues to return an error, the last error is returned.
//   - Successfully generated messages from failed attempts are not replayed on subsequent retries.
//   - Retry behavior (e.g., backoff, which errors are retryable) can be customized via retry.Option.
//   - Context cancellation is respected during retry attempts.
//
// Example usage:
//
//	// Retry up to 5 times with exponential backoff, only on specific errors.
//	mw := Retry(5,
//	    retry.WithBackoff(retry.NewExponentialBackoff()),
//	    retry.WithRetryable(func(err error) bool {
//	        return IsRetryableError(err)
//	    }),
//	)
func Retry(attempts int, opts ...retry.Option) blades.Middleware {
	r := retry.New(attempts, opts...)
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				err := r.Do(ctx, func(ctx context.Context) error {
					// Execute the handler and yield messages
					for msg, err := range next.Handle(ctx, invocation) {
						if err != nil {
							return err
						}
						// Yield successful messages immediately
						if !yield(msg, nil) {
							// Receiver stopped processing
							return nil
						}
					}
					return nil
				})

				// If all retries failed, yield the final error
				if err != nil {
					yield(nil, err)
				}
			}
		})
	}
}
