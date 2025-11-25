package deep

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

var _ tools.Tool = (*writeTodosTool)(nil)

func NewWriteTodosTool() (tools.Tool, string, error) {
	return &writeTodosTool{}, writeTodosToolPrompt, nil
}

type writeTodosTool struct{}

func (t *writeTodosTool) Name() string { return "write_todos" }

func (t *writeTodosTool) Description() string {
	return writeTodosToolDescription
}

type TODO struct {
	Content string `json:"content"`
	Status  string `json:"status" jsonschema:"enum=pending,enum=in_progress,enum=completed"`
}

type writeTodosRequest struct {
	Todos []TODO `json:"todos"`
}

func (t *writeTodosTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"todos"},
		Properties: map[string]*jsonschema.Schema{
			"todos": {
				Type:        "array",
				Description: "The updated todo list",
				Items: &jsonschema.Schema{
					Type:     "object",
					Required: []string{"content", "status"},
					Properties: map[string]*jsonschema.Schema{
						"content": {
							Type:        "string",
							Description: "The task description",
						},
						"status": {
							Type:        "string",
							Description: "The task status",
							Enum:        []any{"pending", "in_progress", "completed"},
						},
					},
				},
			},
		},
	}
}

func (t *writeTodosTool) OutputSchema() *jsonschema.Schema { return nil }

func (t *writeTodosTool) Handle(ctx context.Context, input string) (string, error) {
	req := new(writeTodosRequest)
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", err
	}
	todos, err := json.Marshal(req.Todos)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Updated todo list to %s", todos), nil
}
