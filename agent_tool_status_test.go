package blades

import (
	"context"
	"errors"
	"fmt"
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
	if got, want := toolPart.Completed, true; got != want {
		t.Fatalf("tool completed = %t, want %t", got, want)
	}
	if gotResp, want := toolPart.Response, `{"ok":true}`; gotResp != want {
		t.Fatalf("tool response = %q, want %q", gotResp, want)
	}
}

func TestNewToolPartDefaultsToIncomplete(t *testing.T) {
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
	if got, want := part.Completed, false; got != want {
		t.Fatalf("completed = %t, want %t", got, want)
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
		ID:        "call_1",
		Name:      "echo",
		Request:   `{"value":"hi"}`,
		Response:  `{"cached":true}`,
		Completed: true,
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
	if got, want := toolPart.Completed, true; got != want {
		t.Fatalf("tool completed = %t, want %t", got, want)
	}
	if gotResp, want := toolPart.Response, `{"cached":true}`; gotResp != want {
		t.Fatalf("tool response = %q, want %q", gotResp, want)
	}
}

func TestAgentExecuteToolsCapturesToolErrorsInResponse(t *testing.T) {
	t.Parallel()

	tool := bladestools.NewTool("edit", "edit", bladestools.HandleFunc(func(context.Context, string) (string, error) {
		return "", context.DeadlineExceeded
	}))
	invocation := &Invocation{Tools: []bladestools.Tool{tool}}
	message := NewAssistantMessage(StatusCompleted)
	message.Role = RoleTool
	message.Parts = append(message.Parts, NewToolPart("call_1", "edit", `{"path":"IDENTITY.md"}`))

	_, err := (&agent{}).executeTools(context.Background(), invocation, message)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("executeTools error = %v, want deadline exceeded", err)
	}
}

func TestAgentExecuteToolsTurnsRecoverableErrorsIntoCompletedToolResults(t *testing.T) {
	t.Parallel()

	tool := bladestools.NewTool("edit", "edit", bladestools.HandleFunc(func(context.Context, string) (string, error) {
		return "", fmt.Errorf("edit: edits[0] target not found")
	}))
	invocation := &Invocation{Tools: []bladestools.Tool{tool}}
	message := NewAssistantMessage(StatusCompleted)
	message.Role = RoleTool
	message.Parts = append(message.Parts, NewToolPart("call_1", "edit", `{"path":"IDENTITY.md"}`))

	got, err := (&agent{}).executeTools(context.Background(), invocation, message)
	if err != nil {
		t.Fatalf("executeTools returned error: %v", err)
	}

	toolPart, ok := got.Parts[0].(ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want ToolPart", got.Parts[0])
	}
	if got, want := toolPart.Completed, true; got != want {
		t.Fatalf("tool completed = %t, want %t", got, want)
	}
	if got, want := toolPart.Response, "Tool error: edit: edits[0] target not found"; got != want {
		t.Fatalf("tool response = %q, want %q", got, want)
	}
}
