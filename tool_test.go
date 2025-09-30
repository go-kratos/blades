package blades

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

type TestInput struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type TestOutput struct {
	Message string `json:"message"`
}

func TestTypedHandler(t *testing.T) {
	handler := NewTypedHandler(func(ctx context.Context, input TestInput) (TestOutput, error) {
		return TestOutput{
			Message: fmt.Sprintf("Hello %s, you are %d years old", input.Name, input.Age),
		}, nil
	})

	result, err := handler.Handle(context.Background(), `{"name":"Alice","age":30}`)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expected := `{"message":"Hello Alice, you are 30 years old"}`
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestTypedHandlerInvalidJSON(t *testing.T) {
	handler := NewTypedHandler(func(ctx context.Context, input TestInput) (TestOutput, error) {
		return TestOutput{Message: "test"}, nil
	})

	_, err := handler.Handle(context.Background(), `invalid json`)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestFuncAdapter(t *testing.T) {
	handler := FuncAdapter(func(ctx context.Context, input string) (string, error) {
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

func TestNewTypedTool(t *testing.T) {
	tool := NewTypedTool(
		"test_tool",
		"A test tool",
		&jsonschema.Schema{Type: "object"},
		func(ctx context.Context, input TestInput) (TestOutput, error) {
			msg := fmt.Sprintf("Hello %s, you are %d years old", input.Name, input.Age)
			return TestOutput{Message: msg}, nil
		},
	)

	if tool.Name != "test_tool" {
		t.Errorf("Expected name 'test_tool', got %s", tool.Name)
	}

	if tool.Handler == nil {
		t.Error("Expected Handler to be set")
	}

	// Test the handler works
	result, err := tool.Handler.Handle(context.Background(), `{"name":"Bob","age":25}`)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result != `{"message":"Hello Bob, you are 25 years old"}` {
		t.Errorf("Expected test message, got %s", result)
	}
}

func TestNewFuncTool(t *testing.T) {
	tool := NewFuncTool(
		"func_tool",
		"A function tool",
		&jsonschema.Schema{Type: "object"},
		func(ctx context.Context, input string) (string, error) {
			return "func result: " + input, nil
		},
	)

	if tool.Name != "func_tool" {
		t.Errorf("Expected name 'func_tool', got %s", tool.Name)
	}

	result, err := tool.Handler.Handle(context.Background(), "test input")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expected := "func result: test input"
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

func TestCustomHandler(t *testing.T) {
	handler := &CustomHandler{prefix: "custom: "}
	tool := NewTool(
		"custom_tool",
		"A custom tool",
		&jsonschema.Schema{Type: "object"},
		handler,
	)

	result, err := tool.Handler.Handle(context.Background(), "test")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expected := "custom: test"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}
