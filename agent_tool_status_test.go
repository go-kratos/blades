package blades

import (
	"context"
	"testing"

	bladestools "github.com/go-kratos/blades/tools"
)

func TestAgentExecuteToolsMarksToolPartCompleted(t *testing.T) {
	t.Parallel()

	calls := 0
	tool := bladestools.NewTool("echo", "echo", bladestools.HandleFunc(func(ctx context.Context, input string) (string, error) {
		calls++
		if got, want := input, `{"value":"hi"}`; got != want {
			t.Fatalf("tool input = %q, want %q", got, want)
		}
		return `{"ok":true}`, nil
	}))
	invocation := &Invocation{Tools: []bladestools.Tool{tool}}
	message := NewAssistantMessage(StatusCompleted)
	message.Role = RoleTool
	message.Parts = append(message.Parts, NewToolPart("call_1", "echo", `{"value":"hi"}`))

	got, err := (&agent{}).executeTools(context.Background(), invocation, message)
	if err != nil {
		t.Fatalf("executeTools returned error: %v", err)
	}
	if got != message {
		t.Fatalf("executeTools returned a different message pointer")
	}
	if gotCount, want := calls, 1; gotCount != want {
		t.Fatalf("tool calls = %d, want %d", gotCount, want)
	}

	toolPart, ok := got.Parts[0].(ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want ToolPart", got.Parts[0])
	}
	if gotStatus, want := toolPart.Status, StatusCompleted; gotStatus != want {
		t.Fatalf("tool status = %q, want %q", gotStatus, want)
	}
	if gotResp, want := toolPart.Response, `{"ok":true}`; gotResp != want {
		t.Fatalf("tool response = %q, want %q", gotResp, want)
	}
}

func TestNewToolPartDefaultsToInProgress(t *testing.T) {
	t.Parallel()

	part := NewToolPart("call_1", "echo", `{"value":"hi"}`)

	if got, want := part.ID, "call_1"; got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	if got, want := part.Name, "echo"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := part.Request, `{"value":"hi"}`; got != want {
		t.Fatalf("request = %q, want %q", got, want)
	}
	if got, want := part.Status, StatusInProgress; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got := part.Response; got != "" {
		t.Fatalf("response = %q, want empty", got)
	}
}

func TestAgentExecuteToolsSkipsCompletedToolPart(t *testing.T) {
	t.Parallel()

	calls := 0
	tool := bladestools.NewTool("echo", "echo", bladestools.HandleFunc(func(context.Context, string) (string, error) {
		calls++
		return `{"ok":true}`, nil
	}))
	invocation := &Invocation{Tools: []bladestools.Tool{tool}}
	message := NewAssistantMessage(StatusCompleted)
	message.Role = RoleTool
	message.Parts = append(message.Parts, ToolPart{
		ID:       "call_1",
		Name:     "echo",
		Request:  `{"value":"hi"}`,
		Response: `{"cached":true}`,
		Status:   StatusCompleted,
	})

	got, err := (&agent{}).executeTools(context.Background(), invocation, message)
	if err != nil {
		t.Fatalf("executeTools returned error: %v", err)
	}
	if gotCount, want := calls, 0; gotCount != want {
		t.Fatalf("tool calls = %d, want %d", gotCount, want)
	}

	toolPart, ok := got.Parts[0].(ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want ToolPart", got.Parts[0])
	}
	if gotStatus, want := toolPart.Status, StatusCompleted; gotStatus != want {
		t.Fatalf("tool status = %q, want %q", gotStatus, want)
	}
	if gotResp, want := toolPart.Response, `{"cached":true}`; gotResp != want {
		t.Fatalf("tool response = %q, want %q", gotResp, want)
	}
}
