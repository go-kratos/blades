package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestHandleFunc(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
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

func TestHandleFuncToolCall(t *testing.T) {
	type request struct {
		Location string `json:"location"`
	}
	type response struct {
		Forecast string `json:"forecast"`
	}

	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
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

	tool := NewTool(
		"get_weather",
		"Get current weather",
		handler,
		WithInputSchema(&jsonschema.Schema{Type: "object"}),
	)

	llmArgs := `{"location":"Paris"}`
	result, err := tool.Handle(context.Background(), llmArgs)
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

	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
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
		Handler[string, string]
		prefix string
	}{prefix: "custom: "}

	handler.Handler = HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return handler.prefix + input, nil
	})

	tool := NewTool(
		"custom_tool",
		"A custom tool",
		handler,
		WithInputSchema(&jsonschema.Schema{Type: "object"}),
	)

	result, err := tool.Handle(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result != "custom: test" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestJSONAdapter(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	type res struct {
		Greet string `json:"greet"`
	}

	var h Handler[req, res] = HandleFunc[req, res](func(ctx context.Context, r req) (res, error) {
		return res{Greet: "hi, " + r.Name}, nil
	})

	adapter := JSONAdapter[req, res](h)

	out, err := adapter.Handle(context.Background(), `{"name":"Ada"}`)
	if err != nil {
		t.Fatalf("adapter returned error: %v", err)
	}

	var got res
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("failed to decode adapter output: %v", err)
	}
	if got.Greet != "hi, Ada" {
		t.Fatalf("unexpected greet: %s", got.Greet)
	}
}
