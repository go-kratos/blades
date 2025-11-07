package graph

import (
	"context"
	"time"

	"github.com/go-kratos/exp/backoff"
)

// RetryOption configures the retry middleware.
type RetryOption func(*retryConfig)

// Retryable determines whether an error should trigger another attempt.
type Retryable func(err error) bool

type retryConfig struct {
	attempts  int
	retryable Retryable
	backoff   backoff.Strategy
}

// WithAttempts overrides the maximum number of attempts. A non-positive value means unlimited retries.
func WithAttempts(n int) RetryOption {
	return func(cfg *retryConfig) {
		cfg.attempts = n
	}
}

// WithRetryable sets the predicate used to decide if an error is retryable.
func WithRetryable(r Retryable) RetryOption {
	return func(cfg *retryConfig) {
		cfg.retryable = r
	}
}

// WithBackoff overrides the backoff strategy between attempts.
func WithBackoff(b backoff.Strategy) RetryOption {
	return func(cfg *retryConfig) {
		cfg.backoff = b
	}
}

// Retry returns a middleware that retries node handlers with exponential backoff.
func Retry(opts ...RetryOption) Middleware {
	cfg := &retryConfig{
		attempts:  2,
		retryable: func(error) bool { return true },
		backoff:   backoff.New(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.retryable == nil {
		cfg.retryable = func(error) bool { return true }
	}
	if cfg.backoff == nil {
		cfg.backoff = backoff.New()
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, state State) (State, error) {
			var retries int

			for {
				if err := ctx.Err(); err != nil {
					return nil, err
				}

				nextState, err := next(ctx, state)
				if err == nil {
					return nextState, nil
				}

				if !cfg.retryable(err) {
					return nil, err
				}

				retries++
				if cfg.attempts > 0 && retries >= cfg.attempts {
					return nil, err
				}

				delay := cfg.backoff.Backoff(retries - 1)
				if delay <= 0 {
					continue
				}

				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
}
