package deep

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewWriteTodosTool(t *testing.T) {
	tool, prompt, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	if tool == nil {
		t.Fatal("NewWriteTodosTool() returned nil tool")
	}
	if prompt == "" {
		t.Error("NewWriteTodosTool() returned empty prompt")
	}
	if prompt != writeTodosToolPrompt {
		t.Errorf("NewWriteTodosTool() prompt = %v, want %v", prompt, writeTodosToolPrompt)
	}
}

func TestWriteTodosToolName(t *testing.T) {
	tool, _, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	if tool.Name() != "write_todos" {
		t.Errorf("writeTodosTool.Name() = %v, want 'write_todos'", tool.Name())
	}
}

func TestWriteTodosToolDescription(t *testing.T) {
	tool, _, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("writeTodosTool.Description() returned empty string")
	}
	if desc != writeTodosToolDescription {
		t.Errorf("writeTodosTool.Description() = %v, want %v", desc, writeTodosToolDescription)
	}
}

func TestWriteTodosToolInputSchema(t *testing.T) {
	tool, _, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	schema := tool.InputSchema()
	if schema == nil {
		t.Fatal("writeTodosTool.InputSchema() returned nil")
	}
	if schema.Type != "object" {
		t.Errorf("InputSchema.Type = %v, want 'object'", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "todos" {
		t.Errorf("InputSchema.Required = %v, want ['todos']", schema.Required)
	}
	if schema.Properties == nil {
		t.Fatal("InputSchema.Properties is nil")
	}
	todosSchema, ok := schema.Properties["todos"]
	if !ok {
		t.Fatal("InputSchema.Properties missing 'todos'")
	}
	if todosSchema.Type != "array" {
		t.Errorf("todos schema type = %v, want 'array'", todosSchema.Type)
	}
	if todosSchema.Items == nil {
		t.Fatal("todos schema items is nil")
	}
	itemSchema := todosSchema.Items
	if itemSchema.Type != "object" {
		t.Errorf("todo item schema type = %v, want 'object'", itemSchema.Type)
	}
	requiredFields := []string{"content", "status"}
	if len(itemSchema.Required) != len(requiredFields) {
		t.Errorf("todo item schema required length = %v, want %v", len(itemSchema.Required), len(requiredFields))
	}
	for _, field := range requiredFields {
		found := false
		for _, req := range itemSchema.Required {
			if req == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("todo item schema required missing field: %s", field)
		}
	}
	if itemSchema.Properties == nil {
		t.Fatal("todo item schema properties is nil")
	}
	contentProp, ok := itemSchema.Properties["content"]
	if !ok {
		t.Error("todo item schema properties missing 'content'")
	} else if contentProp.Type != "string" {
		t.Errorf("content property type = %v, want 'string'", contentProp.Type)
	}
	statusProp, ok := itemSchema.Properties["status"]
	if !ok {
		t.Error("todo item schema properties missing 'status'")
	} else {
		if statusProp.Type != "string" {
			t.Errorf("status property type = %v, want 'string'", statusProp.Type)
		}
		expectedEnum := []any{"pending", "in_progress", "completed"}
		if len(statusProp.Enum) != len(expectedEnum) {
			t.Errorf("status property enum length = %v, want %v", len(statusProp.Enum), len(expectedEnum))
		}
	}
}

func TestWriteTodosToolOutputSchema(t *testing.T) {
	tool, _, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	schema := tool.OutputSchema()
	if schema != nil {
		t.Errorf("writeTodosTool.OutputSchema() = %v, want nil", schema)
	}
}

func TestWriteTodosToolHandle(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkResp func(t *testing.T, resp string)
	}{
		{
			name:    "valid single todo",
			input:   `{"todos":[{"content":"test task","status":"pending"}]}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if !strings.Contains(resp, "Updated todo list") {
					t.Errorf("Handle() response = %v, want to contain 'Updated todo list'", resp)
				}
				if !strings.Contains(resp, "test task") {
					t.Errorf("Handle() response = %v, want to contain 'test task'", resp)
				}
				if !strings.Contains(resp, "pending") {
					t.Errorf("Handle() response = %v, want to contain 'pending'", resp)
				}
			},
		},
		{
			name:    "valid multiple todos",
			input:   `{"todos":[{"content":"task 1","status":"pending"},{"content":"task 2","status":"in_progress"},{"content":"task 3","status":"completed"}]}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if !strings.Contains(resp, "task 1") {
					t.Errorf("Handle() response missing 'task 1'")
				}
				if !strings.Contains(resp, "task 2") {
					t.Errorf("Handle() response missing 'task 2'")
				}
				if !strings.Contains(resp, "task 3") {
					t.Errorf("Handle() response missing 'task 3'")
				}
			},
		},
		{
			name:    "empty todos array",
			input:   `{"todos":[]}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if !strings.Contains(resp, "Updated todo list") {
					t.Errorf("Handle() response = %v, want to contain 'Updated todo list'", resp)
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   `{invalid json}`,
			wantErr: true,
		},
		{
			name:    "missing todos field",
			input:   `{"other":"field"}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if !strings.Contains(resp, "Updated todo list") {
					t.Errorf("Handle() response = %v, want to contain 'Updated todo list'", resp)
				}
			},
		},
		{
			name:    "todos with all statuses",
			input:   `{"todos":[{"content":"pending task","status":"pending"},{"content":"active task","status":"in_progress"},{"content":"done task","status":"completed"}]}`,
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				statuses := []string{"pending", "in_progress", "completed"}
				for _, status := range statuses {
					if !strings.Contains(resp, status) {
						t.Errorf("Handle() response missing status: %s", status)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, _, err := NewWriteTodosTool()
			if err != nil {
				t.Fatalf("NewWriteTodosTool() returned error: %v", err)
			}
			resp, err := tool.Handle(context.Background(), tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Handle() expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Handle() unexpected error: %v", err)
				return
			}
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}

func TestWriteTodosToolHandleWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool, _, err := NewWriteTodosTool()
	if err != nil {
		t.Fatalf("NewWriteTodosTool() returned error: %v", err)
	}
	input := `{"todos":[{"content":"test task","status":"pending"}]}`
	resp, err := tool.Handle(ctx, input)
	if err != nil {
		t.Errorf("Handle() unexpected error: %v", err)
	}
	if !strings.Contains(resp, "Updated todo list") {
		t.Errorf("Handle() response = %v, want to contain 'Updated todo list'", resp)
	}
}

func TestTODOStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TODO
		wantErr bool
	}{
		{
			name:  "valid pending todo",
			input: `{"content":"test task","status":"pending"}`,
			want: TODO{
				Content: "test task",
				Status:  "pending",
			},
			wantErr: false,
		},
		{
			name:  "valid in_progress todo",
			input: `{"content":"active task","status":"in_progress"}`,
			want: TODO{
				Content: "active task",
				Status:  "in_progress",
			},
			wantErr: false,
		},
		{
			name:  "valid completed todo",
			input: `{"content":"done task","status":"completed"}`,
			want: TODO{
				Content: "done task",
				Status:  "completed",
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:  "empty fields",
			input: `{"content":"","status":""}`,
			want: TODO{
				Content: "",
				Status:  "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var todo TODO
			err := json.Unmarshal([]byte(tt.input), &todo)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Unmarshal() expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unmarshal() unexpected error: %v", err)
				return
			}
			if todo.Content != tt.want.Content {
				t.Errorf("TODO.Content = %v, want %v", todo.Content, tt.want.Content)
			}
			if todo.Status != tt.want.Status {
				t.Errorf("TODO.Status = %v, want %v", todo.Status, tt.want.Status)
			}
		})
	}
}

func TestWriteTodosRequestStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    writeTodosRequest
		wantErr bool
	}{
		{
			name:  "valid request with single todo",
			input: `{"todos":[{"content":"task 1","status":"pending"}]}`,
			want: writeTodosRequest{
				Todos: []TODO{
					{Content: "task 1", Status: "pending"},
				},
			},
			wantErr: false,
		},
		{
			name:  "valid request with multiple todos",
			input: `{"todos":[{"content":"task 1","status":"pending"},{"content":"task 2","status":"completed"}]}`,
			want: writeTodosRequest{
				Todos: []TODO{
					{Content: "task 1", Status: "pending"},
					{Content: "task 2", Status: "completed"},
				},
			},
			wantErr: false,
		},
		{
			name:  "valid request with empty todos",
			input: `{"todos":[]}`,
			want: writeTodosRequest{
				Todos: []TODO{},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req writeTodosRequest
			err := json.Unmarshal([]byte(tt.input), &req)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Unmarshal() expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unmarshal() unexpected error: %v", err)
				return
			}
			if len(req.Todos) != len(tt.want.Todos) {
				t.Errorf("writeTodosRequest.Todos length = %v, want %v", len(req.Todos), len(tt.want.Todos))
				return
			}
			for i, todo := range req.Todos {
				if todo.Content != tt.want.Todos[i].Content {
					t.Errorf("TODO[%d].Content = %v, want %v", i, todo.Content, tt.want.Todos[i].Content)
				}
				if todo.Status != tt.want.Todos[i].Status {
					t.Errorf("TODO[%d].Status = %v, want %v", i, todo.Status, tt.want.Todos[i].Status)
				}
			}
		})
	}
}
