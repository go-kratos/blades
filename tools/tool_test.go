package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestTool(t *testing.T) {
	tool := &Tool{
		Name:         "test-tool",
		Description:  "A test tool",
		InputSchema:  &jsonschema.Schema{Type: "object"},
		OutputSchema: &jsonschema.Schema{Type: "string"},
		Handler: HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
			return "processed: " + input, nil
		}),
	}

	if tool.Name != "test-tool" {
		t.Errorf("Tool.Name = %v, want test-tool", tool.Name)
	}
	if tool.Description != "A test tool" {
		t.Errorf("Tool.Description = %v, want A test tool", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("Tool.InputSchema should not be nil")
	}
	if tool.OutputSchema == nil {
		t.Errorf("Tool.OutputSchema should not be nil")
	}
	if tool.Handler == nil {
		t.Errorf("Tool.Handler should not be nil")
	}
}

func TestNewTool(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Greeting string `json:"greeting"`
	}

	handler := HandleFunc[request, response](func(ctx context.Context, req request) (response, error) {
		return response{Greeting: "Hello, " + req.Name + "!"}, nil
	})

	tool, err := NewTool("test-tool", "A test tool", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	if tool.Name != "test-tool" {
		t.Errorf("Tool.Name = %v, want test-tool", tool.Name)
	}
	if tool.Description != "A test tool" {
		t.Errorf("Tool.Description = %v, want A test tool", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("Tool.InputSchema should not be nil")
	}
	if tool.OutputSchema == nil {
		t.Errorf("Tool.OutputSchema should not be nil")
	}
	if tool.Handler == nil {
		t.Errorf("Tool.Handler should not be nil")
	}
}

func TestNewToolWithComplexTypes(t *testing.T) {
	type request struct {
		Items []string `json:"items"`
		Count int      `json:"count"`
	}
	type response struct {
		Result []string `json:"result"`
		Total  int      `json:"total"`
	}

	handler := HandleFunc[request, response](func(ctx context.Context, req request) (response, error) {
		return response{
			Result: req.Items,
			Total:  req.Count,
		}, nil
	})

	tool, err := NewTool("complex-tool", "A complex tool", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	if tool.Name != "complex-tool" {
		t.Errorf("Tool.Name = %v, want complex-tool", tool.Name)
	}
	if tool.Description != "A complex tool" {
		t.Errorf("Tool.Description = %v, want A complex tool", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("Tool.InputSchema should not be nil")
	}
	if tool.OutputSchema == nil {
		t.Errorf("Tool.OutputSchema should not be nil")
	}
}

func TestNewToolWithPrimitiveTypes(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	tool, err := NewTool("primitive-tool", "A primitive tool", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	if tool.Name != "primitive-tool" {
		t.Errorf("Tool.Name = %v, want primitive-tool", tool.Name)
	}
	if tool.Description != "A primitive tool" {
		t.Errorf("Tool.Description = %v, want A primitive tool", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("Tool.InputSchema should not be nil")
	}
	if tool.OutputSchema == nil {
		t.Errorf("Tool.OutputSchema should not be nil")
	}
}

func TestNewToolWithEmptyName(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	tool, err := NewTool("", "A tool with empty name", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	if tool.Name != "" {
		t.Errorf("Tool.Name = %v, want empty string", tool.Name)
	}
}

func TestNewToolWithEmptyDescription(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	tool, err := NewTool("test-tool", "", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	if tool.Description != "" {
		t.Errorf("Tool.Description = %v, want empty string", tool.Description)
	}
}

// TestNewToolWithNilHandler is skipped because it causes a panic
// This is expected behavior when passing nil to NewTool

func TestToolHandler(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	tool := &Tool{
		Name:         "test-tool",
		Description:  "A test tool",
		InputSchema:  &jsonschema.Schema{Type: "object"},
		OutputSchema: &jsonschema.Schema{Type: "string"},
		Handler:      handler,
	}

	result, err := tool.Handler.Handle(context.Background(), "test")
	if err != nil {
		t.Errorf("Tool.Handler.Handle() returned error: %v", err)
	}

	expected := "processed: test"
	if result != expected {
		t.Errorf("Tool.Handler.Handle() = %v, want %v", result, expected)
	}
}

func TestToolHandlerWithError(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		return "", errors.New("handler error")
	})

	tool := &Tool{
		Name:         "test-tool",
		Description:  "A test tool",
		InputSchema:  &jsonschema.Schema{Type: "object"},
		OutputSchema: &jsonschema.Schema{Type: "string"},
		Handler:      handler,
	}

	_, err := tool.Handler.Handle(context.Background(), "test")
	if err == nil {
		t.Errorf("Tool.Handler.Handle() should return error")
	}
	if err.Error() != "handler error" {
		t.Errorf("Tool.Handler.Handle() error = %v, want handler error", err)
	}
}

func TestToolHandlerWithContext(t *testing.T) {
	handler := HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
		// Check if context is passed correctly
		if ctx == nil {
			return "", errors.New("context is nil")
		}
		return "context ok: " + input, nil
	})

	tool := &Tool{
		Name:         "test-tool",
		Description:  "A test tool",
		InputSchema:  &jsonschema.Schema{Type: "object"},
		OutputSchema: &jsonschema.Schema{Type: "string"},
		Handler:      handler,
	}

	result, err := tool.Handler.Handle(context.Background(), "test")
	if err != nil {
		t.Errorf("Tool.Handler.Handle() returned error: %v", err)
	}

	expected := "context ok: test"
	if result != expected {
		t.Errorf("Tool.Handler.Handle() = %v, want %v", result, expected)
	}
}

func TestToolHandlerWithJSONAdapter(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Greeting string `json:"greeting"`
	}

	handler := HandleFunc[request, response](func(ctx context.Context, req request) (response, error) {
		return response{Greeting: "Hello, " + req.Name + "!"}, nil
	})

	tool, err := NewTool("test-tool", "A test tool", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	// Test with valid JSON
	input := `{"name":"Alice"}`
	result, err := tool.Handler.Handle(context.Background(), input)
	if err != nil {
		t.Errorf("Tool.Handler.Handle() returned error: %v", err)
	}

	var resp response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Errorf("Failed to unmarshal response: %v", err)
	}

	expected := "Hello, Alice!"
	if resp.Greeting != expected {
		t.Errorf("Response.Greeting = %v, want %v", resp.Greeting, expected)
	}
}

func TestToolHandlerWithInvalidJSON(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Greeting string `json:"greeting"`
	}

	handler := HandleFunc[request, response](func(ctx context.Context, req request) (response, error) {
		return response{Greeting: "Hello, " + req.Name + "!"}, nil
	})

	tool, err := NewTool("test-tool", "A test tool", handler)
	if err != nil {
		t.Errorf("NewTool() returned error: %v", err)
	}

	// Test with invalid JSON
	input := `{"name":123}` // name should be string, not number
	_, err = tool.Handler.Handle(context.Background(), input)
	if err == nil {
		t.Errorf("Tool.Handler.Handle() should return error for invalid JSON")
	}
}
