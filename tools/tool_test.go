package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestHandleFunc(t *testing.T) {
	handler := HandleFunc(func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	result, err := handler.Handle(context.Background(), "test")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expected := "processed: test"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

// Test custom ToolHandler implementation
type CustomHandler struct {
	prefix string
}

func (h *CustomHandler) Handle(ctx context.Context, input string) (string, error) {
	return h.prefix + input, nil
}

func TestHandleFuncToolCall(t *testing.T) {
	type request struct {
		Location string `json:"location"`
	}
	type response struct {
		Forecast string `json:"forecast"`
	}

	handler := HandleFunc(func(ctx context.Context, input string) (string, error) {
		var payload request
		if err := json.Unmarshal([]byte(input), &payload); err != nil {
			return "", err
		}

		result := response{Forecast: "Sunny in " + payload.Location}
		encoded, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	})

	tool := &Tool{
		Name:        "get_weather",
		Description: "Get current weather",
		InputSchema: &jsonschema.Schema{Type: "object"},
		Handler:     handler,
	}

	llmArgs := `{"location":"Paris"}`
	result, err := tool.Handler.Handle(context.Background(), llmArgs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var decoded response
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("failed to decode tool result: %v", err)
	}

	if decoded.Forecast != "Sunny in Paris" {
		t.Fatalf("unexpected forecast: %s", decoded.Forecast)
	}
}

func TestHandleFuncInvalidPayload(t *testing.T) {
	type request struct {
		Location string `json:"location"`
	}

	handler := HandleFunc(func(ctx context.Context, input string) (string, error) {
		var payload request
		if err := json.Unmarshal([]byte(input), &payload); err != nil {
			return "", err
		}
		return "{}", nil
	})

	// location should be a string, but the LLM returned a number, so JSON decoding must fail.
	_, err := handler.Handle(context.Background(), `{"location":123}`)
	if err == nil {
		t.Fatal("expected error when payload contains invalid types")
	}
}

func TestCustomHandler(t *testing.T) {
	handler := &struct {
		Handler
		prefix string
	}{prefix: "custom: "}

	handler.Handler = HandleFunc(func(ctx context.Context, input string) (string, error) {
		return handler.prefix + input, nil
	})

	tool := &Tool{
		Name:        "custom_tool",
		Description: "A custom tool",
		InputSchema: &jsonschema.Schema{Type: "object"},
		Handler:     handler,
	}

	result, err := tool.Handler.Handle(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result != "custom: test" {
		t.Fatalf("unexpected result: %s", result)
	}
}
