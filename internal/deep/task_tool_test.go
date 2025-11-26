package deep

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
)

// mockAgent is an Agent mock implementation for testing
type mockAgent struct {
	name        string
	description string
	runFunc     func(context.Context, *blades.Invocation) iter.Seq2[*blades.Message, error]
}

func (m *mockAgent) Name() string {
	return m.name
}

func (m *mockAgent) Description() string {
	return m.description
}

func (m *mockAgent) Run(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
	if m.runFunc != nil {
		return m.runFunc(ctx, inv)
	}
	return func(yield func(*blades.Message, error) bool) {
		yield(blades.AssistantMessage("mock response"), nil)
	}
}

func TestNewTaskTool(t *testing.T) {
	tests := []struct {
		name    string
		config  TaskToolConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "single subagent",
			config: TaskToolConfig{
				SubAgents: []blades.Agent{
					&mockAgent{name: "test-agent", description: "test agent"},
				},
				WithoutGeneralSubAgent: true,
			},
			wantErr: false,
		},
		{
			name: "multiple subagents",
			config: TaskToolConfig{
				SubAgents: []blades.Agent{
					&mockAgent{name: "agent1", description: "first agent"},
					&mockAgent{name: "agent2", description: "second agent"},
					&mockAgent{name: "agent3", description: "third agent"},
				},
				WithoutGeneralSubAgent: true,
			},
			wantErr: false,
		},
		{
			name: "empty subagents list without general agent",
			config: TaskToolConfig{
				SubAgents:              []blades.Agent{},
				WithoutGeneralSubAgent: true,
			},
			wantErr: false,
		},
		{
			name: "nil subagents list without general agent",
			config: TaskToolConfig{
				SubAgents:              nil,
				WithoutGeneralSubAgent: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, prompt, err := NewTaskTool(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewTaskTool() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("NewTaskTool() error = %v, want to contain %v", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("NewTaskTool() unexpected error: %v", err)
				return
			}
			if tool == nil {
				t.Errorf("NewTaskTool() returned nil tool")
				return
			}
			if prompt != taskPrompt {
				t.Errorf("NewTaskTool() prompt = %v, want %v", prompt, taskPrompt)
			}
		})
	}
}

func TestTaskToolName(t *testing.T) {
	config := TaskToolConfig{
		SubAgents: []blades.Agent{
			&mockAgent{name: "test", description: "test"},
		},
		WithoutGeneralSubAgent: true,
	}
	tool, _, err := NewTaskTool(config)
	if err != nil {
		t.Fatalf("NewTaskTool() returned error: %v", err)
	}
	if tool.Name() != "task" {
		t.Errorf("taskTool.Name() = %v, want 'task'", tool.Name())
	}
}

func TestTaskToolDescription(t *testing.T) {
	subAgents := []blades.Agent{
		&mockAgent{name: "agent1", description: "first test agent"},
		&mockAgent{name: "agent2", description: "second test agent"},
	}
	config := TaskToolConfig{
		SubAgents:              subAgents,
		WithoutGeneralSubAgent: true,
	}
	tool, _, err := NewTaskTool(config)
	if err != nil {
		t.Fatalf("NewTaskTool() returned error: %v", err)
	}
	desc := tool.Description()
	if desc == "" {
		t.Error("taskTool.Description() returned empty string")
	}
	for _, agent := range subAgents {
		if !strings.Contains(desc, agent.Name()) {
			t.Errorf("description missing agent name: %s", agent.Name())
		}
		if !strings.Contains(desc, agent.Description()) {
			t.Errorf("description missing agent description: %s", agent.Description())
		}
	}
}

func TestTaskToolInputSchema(t *testing.T) {
	config := TaskToolConfig{
		SubAgents: []blades.Agent{
			&mockAgent{name: "test", description: "test"},
		},
		WithoutGeneralSubAgent: true,
	}
	tool, _, err := NewTaskTool(config)
	if err != nil {
		t.Fatalf("NewTaskTool() returned error: %v", err)
	}
	schema := tool.InputSchema()
	if schema == nil {
		t.Fatal("taskTool.InputSchema() returned nil")
	}
	if schema.Type != "object" {
		t.Errorf("InputSchema.Type = %v, want 'object'", schema.Type)
	}
	requiredFields := []string{"subagent_type", "description"}
	if len(schema.Required) != len(requiredFields) {
		t.Errorf("InputSchema.Required length = %v, want %v", len(schema.Required), len(requiredFields))
	}
	for _, field := range requiredFields {
		found := false
		for _, req := range schema.Required {
			if req == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InputSchema.Required missing field: %s", field)
		}
	}
	if schema.Properties == nil {
		t.Fatal("InputSchema.Properties is nil")
	}
	if _, ok := schema.Properties["subagent_type"]; !ok {
		t.Error("InputSchema.Properties missing 'subagent_type'")
	}
	if _, ok := schema.Properties["description"]; !ok {
		t.Error("InputSchema.Properties missing 'description'")
	}
}

func TestTaskToolOutputSchema(t *testing.T) {
	config := TaskToolConfig{
		SubAgents: []blades.Agent{
			&mockAgent{name: "test", description: "test"},
		},
		WithoutGeneralSubAgent: true,
	}
	tool, _, err := NewTaskTool(config)
	if err != nil {
		t.Fatalf("NewTaskTool() returned error: %v", err)
	}
	schema := tool.OutputSchema()
	if schema != nil {
		t.Errorf("taskTool.OutputSchema() = %v, want nil", schema)
	}
}

func TestTaskToolHandle(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		agents    []blades.Agent
		wantErr   bool
		errMsg    string
		checkResp func(t *testing.T, resp string)
	}{
		{
			name:  "successfully call existing subagent",
			input: `{"subagent_type":"test-agent","description":"test task"}`,
			agents: []blades.Agent{
				&mockAgent{
					name:        "test-agent",
					description: "test agent",
					runFunc: func(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
						return func(yield func(*blades.Message, error) bool) {
							yield(blades.AssistantMessage("task completed"), nil)
						}
					},
				},
			},
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if resp != "task completed" {
					t.Errorf("Handle() response = %v, want 'task completed'", resp)
				}
			},
		},
		{
			name:  "nonexistent subagent should return error",
			input: `{"subagent_type":"nonexistent","description":"test task"}`,
			agents: []blades.Agent{
				&mockAgent{name: "test-agent", description: "test agent"},
			},
			wantErr: true,
			errMsg:  "subagent type nonexistent not found",
		},
		{
			name:  "invalid JSON input should return error",
			input: `{invalid json}`,
			agents: []blades.Agent{
				&mockAgent{name: "test-agent", description: "test agent"},
			},
			wantErr: true,
		},
		{
			name:  "missing required field should work",
			input: `{"subagent_type":"test-agent"}`,
			agents: []blades.Agent{
				&mockAgent{
					name:        "test-agent",
					description: "test agent",
					runFunc: func(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
						return func(yield func(*blades.Message, error) bool) {
							yield(blades.AssistantMessage(""), nil)
						}
					},
				},
			},
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
			},
		},
		{
			name:  "select correct subagent from multiple",
			input: `{"subagent_type":"agent2","description":"run agent2"}`,
			agents: []blades.Agent{
				&mockAgent{
					name:        "agent1",
					description: "first agent",
					runFunc: func(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
						return func(yield func(*blades.Message, error) bool) {
							yield(blades.AssistantMessage("agent1 response"), nil)
						}
					},
				},
				&mockAgent{
					name:        "agent2",
					description: "second agent",
					runFunc: func(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
						return func(yield func(*blades.Message, error) bool) {
							yield(blades.AssistantMessage("agent2 response"), nil)
						}
					},
				},
			},
			wantErr: false,
			checkResp: func(t *testing.T, resp string) {
				if resp != "agent2 response" {
					t.Errorf("Handle() response = %v, want 'agent2 response'", resp)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := TaskToolConfig{
				SubAgents:              tt.agents,
				WithoutGeneralSubAgent: true,
			}
			tool, _, err := NewTaskTool(config)
			if err != nil {
				t.Fatalf("NewTaskTool() returned error: %v", err)
			}
			resp, err := tool.Handle(context.Background(), tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Handle() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Handle() error = %v, want to contain %v", err.Error(), tt.errMsg)
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

func TestTaskToolHandleWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent := &mockAgent{
		name:        "test-agent",
		description: "test agent",
		runFunc: func(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				select {
				case <-ctx.Done():
					yield(nil, ctx.Err())
				default:
					yield(blades.AssistantMessage("success"), nil)
				}
			}
		},
	}
	config := TaskToolConfig{
		SubAgents:              []blades.Agent{agent},
		WithoutGeneralSubAgent: true,
	}
	tool, _, err := NewTaskTool(config)
	if err != nil {
		t.Fatalf("NewTaskTool() returned error: %v", err)
	}
	input := `{"subagent_type":"test-agent","description":"test context"}`
	resp, err := tool.Handle(ctx, input)
	if err != nil {
		t.Errorf("Handle() unexpected error: %v", err)
	}
	if resp != "success" {
		t.Errorf("Handle() response = %v, want 'success'", resp)
	}
}

func TestTaskToolBuildDescription(t *testing.T) {
	agents := []blades.Agent{
		&mockAgent{name: "agent1", description: "description1"},
		&mockAgent{name: "agent2", description: "description2"},
	}
	tool := &taskTool{
		subAgents:    agents,
		subAgentsMap: make(map[string]blades.Agent),
	}
	desc, err := tool.buildDescription()
	if err != nil {
		t.Fatalf("buildDescription() returned error: %v", err)
	}
	if desc == "" {
		t.Error("buildDescription() returned empty string")
	}
	for _, agent := range agents {
		if !strings.Contains(desc, agent.Name()) {
			t.Errorf("description missing agent name: %s", agent.Name())
		}
		if !strings.Contains(desc, agent.Description()) {
			t.Errorf("description missing agent description: %s", agent.Description())
		}
	}
}

func TestTaskToolRequestUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantReq taskToolRequest
		wantErr bool
	}{
		{
			name:  "valid JSON",
			input: `{"subagent_type":"test","description":"test description"}`,
			wantReq: taskToolRequest{
				SubagentType: "test",
				Description:  "test description",
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:  "empty field values",
			input: `{"subagent_type":"","description":""}`,
			wantReq: taskToolRequest{
				SubagentType: "",
				Description:  "",
			},
			wantErr: false,
		},
		{
			name:  "extra fields should be ignored",
			input: `{"subagent_type":"test","description":"desc","extra":"field"}`,
			wantReq: taskToolRequest{
				SubagentType: "test",
				Description:  "desc",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req taskToolRequest
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
			if req.SubagentType != tt.wantReq.SubagentType {
				t.Errorf("SubagentType = %v, want %v", req.SubagentType, tt.wantReq.SubagentType)
			}
			if req.Description != tt.wantReq.Description {
				t.Errorf("Description = %v, want %v", req.Description, tt.wantReq.Description)
			}
		})
	}
}
