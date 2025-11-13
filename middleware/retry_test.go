package middleware

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/kit/retry"
)

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	middleware := Retry(3)

	called := 0
	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		called++
		return func(yield func(*blades.Message, error) bool) {
			msg := blades.AssistantMessage("success")
			if !yield(msg, nil) {
				return
			}
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var messages []*blades.Message
	var lastErr error

	for msg, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
		messages = append(messages, msg)
	}

	if called != 1 {
		t.Errorf("expected handler to be called once, got %d", called)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Text() != "success" {
		t.Errorf("expected message content 'success', got '%s'", messages[0].Text())
	}
	if lastErr != nil {
		t.Errorf("expected no error, got %v", lastErr)
	}
}

func TestRetry_RetryThenSuccess(t *testing.T) {
	middleware := Retry(3)

	attempts := 0
	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		attempts++
		return func(yield func(*blades.Message, error) bool) {
			if attempts < 2 {
				yield(nil, errors.New("temporary failure"))
				return
			}
			msg := blades.AssistantMessage("success after retry")
			if !yield(msg, nil) {
				return
			}
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var messages []*blades.Message
	var lastErr error

	for msg, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
		messages = append(messages, msg)
	}

	if attempts != 2 {
		t.Errorf("expected handler to be called twice, got %d", attempts)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Text() != "success after retry" {
		t.Errorf("expected message content 'success after retry', got '%s'", messages[0].Text())
	}
	if lastErr != nil {
		t.Errorf("expected no error, got %v", lastErr)
	}
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	middleware := Retry(2)

	attempts := 0
	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		attempts++
		return func(yield func(*blades.Message, error) bool) {
			yield(nil, errors.New("persistent failure"))
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var messages []*blades.Message
	var lastErr error

	for msg, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
		messages = append(messages, msg)
	}

	if attempts != 2 {
		t.Errorf("expected handler to be called twice, got %d", attempts)
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
	if lastErr == nil {
		t.Errorf("expected error, got none")
	}
	if lastErr.Error() != "persistent failure" {
		t.Errorf("expected error message 'persistent failure', got '%s'", lastErr.Error())
	}
}

func TestRetry_WithCustomRetryable(t *testing.T) {
	middleware := Retry(3,
		retry.WithRetryable(func(err error) bool {
			return err.Error() == "retryable error"
		}),
	)

	attempts := 0
	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		attempts++
		return func(yield func(*blades.Message, error) bool) {
			yield(nil, errors.New("non-retryable error"))
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var lastErr error
	for _, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
	}

	if attempts != 1 {
		t.Errorf("expected handler to be called once for non-retryable error, got %d", attempts)
	}
	if lastErr == nil {
		t.Errorf("expected error, got none")
	}
	if lastErr.Error() != "non-retryable error" {
		t.Errorf("expected error message 'non-retryable error', got '%s'", lastErr.Error())
	}
}

func TestRetry_WithContextCancellation(t *testing.T) {
	middleware := Retry(5)

	attempts := 0
	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		attempts++
		return func(yield func(*blades.Message, error) bool) {
			select {
			case <-ctx.Done():
				yield(nil, ctx.Err())
				return
			default:
				yield(nil, errors.New("always fails"))
			}
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	start := time.Now()
	var lastErr error
	for _, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
	}
	elapsed := time.Since(start)

	// Should be cancelled quickly, not wait for all 5 retry attempts
	if elapsed >= 400*time.Millisecond {
		t.Errorf("context cancellation not respected, took %v", elapsed)
	}

	if lastErr == nil {
		t.Errorf("expected error due to context cancellation, got none")
	}
	if !errors.Is(lastErr, context.DeadlineExceeded) && !errors.Is(lastErr, context.Canceled) {
		t.Errorf("expected context error, got %v", lastErr)
	}
}

func TestRetry_HandlerReturnsMultipleMessages(t *testing.T) {
	middleware := Retry(2)

	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		return func(yield func(*blades.Message, error) bool) {
			// Yield multiple messages
			for i := 0; i < 3; i++ {
				msg := blades.AssistantMessage(fmt.Sprintf("message %d", i))
				if !yield(msg, nil) {
					return
				}
			}
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var messages []*blades.Message
	var lastErr error

	for msg, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
		messages = append(messages, msg)
	}

	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
	for i, msg := range messages {
		expectedText := "message " + string(rune('0'+i))
		if msg.Text() != expectedText {
			t.Errorf("expected message %d content '%s', got '%s'", i, expectedText, msg.Text())
		}
	}
	if lastErr != nil {
		t.Errorf("expected no error, got %v", lastErr)
	}
}

func TestRetry_ReceiverStopsProcessing(t *testing.T) {
	middleware := Retry(3)

	handler := middleware(blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		return func(yield func(*blades.Message, error) bool) {
			// Yield multiple messages
			for i := 0; i < 10; i++ {
				msg := blades.AssistantMessage(fmt.Sprintf("message %d", i))
				if !yield(msg, nil) {
					// Receiver stopped processing
					return
				}
			}
		}
	}))

	invocation := &blades.Invocation{
		ID:      "test",
		Message: blades.UserMessage("test"),
	}

	ctx := context.Background()
	var messages []*blades.Message
	var lastErr error
	count := 0

	// Only process first 3 messages
	for msg, err := range handler.Handle(ctx, invocation) {
		if err != nil {
			lastErr = err
			break
		}
		messages = append(messages, msg)
		count++
		if count >= 3 {
			break
		}
	}

	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
	if lastErr != nil {
		t.Errorf("expected no error, got %v", lastErr)
	}
}
