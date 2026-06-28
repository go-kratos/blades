package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/blades/tools"
)

func TestTruncate_UnderLimit(t *testing.T) {
	truncMw := tools.Truncate(100)
	handler := truncMw(tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		return "short result", nil
	}))

	result, err := handler.Handle(context.Background(), "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "short result" {
		t.Errorf("expected 'short result', got '%s'", result)
	}
}

func TestTruncate_OverLimit(t *testing.T) {
	truncMw := tools.Truncate(10)
	longResult := strings.Repeat("x", 50)
	handler := truncMw(tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		return longResult, nil
	}))

	result, err := handler.Handle(context.Background(), "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) < len(longResult) {
		t.Logf("result was truncated (expected): %d -> %d chars", len(longResult), len(result))
	} else {
		t.Errorf("result should have been truncated")
	}
	if !strings.Contains(result, "Output truncated") {
		t.Errorf("result should contain truncation notice, got: %s", result)
	}
}

func TestTruncate_ExactlyAtLimit(t *testing.T) {
	truncMw := tools.Truncate(5)
	exact := "hello"
	handler := truncMw(tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		return exact, nil
	}))

	result, err := handler.Handle(context.Background(), "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got '%s'", result)
	}
}

func TestTruncate_ErrorPassthrough(t *testing.T) {
	truncMw := tools.Truncate(100)
	handler := truncMw(tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		return "", context.DeadlineExceeded
	}))

	_, err := handler.Handle(context.Background(), "any")
	if err == nil {
		t.Fatal("expected error to pass through")
	}
}

func TestTruncate_ChainWithOtherMiddleware(t *testing.T) {
	// Truncate can be chained with other middlewares
	callCount := 0
	countMw := func(next tools.Handler) tools.Handler {
		return tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
			callCount++
			return next.Handle(ctx, input)
		})
	}

	truncMw := tools.Truncate(10)
	chained := tools.ChainMiddlewares(countMw, truncMw)

	handler := chained(tools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		return strings.Repeat("x", 50), nil
	}))

	result, err := handler.Handle(context.Background(), "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if !strings.Contains(result, "Output truncated") {
		t.Errorf("expected truncation, got: %s", result)
	}
}
