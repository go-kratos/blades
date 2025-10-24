package blades

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestNewAgent(t *testing.T) {
	agent := NewAgent("test-agent")

	if agent.name != "test-agent" {
		t.Errorf("Agent.name = %v, want test-agent", agent.name)
	}
	if agent.maxIterations != 10 {
		t.Errorf("Agent.maxIterations = %v, want 10", agent.maxIterations)
	}
	if agent.middleware == nil {
		t.Errorf("Agent.middleware should not be nil")
	}
	if agent.inputHandler == nil {
		t.Errorf("Agent.inputHandler should not be nil")
	}
	if agent.outputHandler == nil {
		t.Errorf("Agent.outputHandler should not be nil")
	}
}

func TestNewAgentWithOptions(t *testing.T) {
	model := "test-model"
	description := "Test agent description"
	instructions := "Test instructions"
	outputKey := "test-output"
	maxIterations := 5

	schema := &jsonschema.Schema{Type: "object"}

	agent := NewAgent("test-agent",
		WithModel(model),
		WithDescription(description),
		WithInstructions(instructions),
		WithOutputKey(outputKey),
		WithMaxIterations(maxIterations),
		WithInputSchema(schema),
		WithOutputSchema(schema),
	)

	if agent.model != model {
		t.Errorf("Agent.model = %v, want %v", agent.model, model)
	}
	if agent.description != description {
		t.Errorf("Agent.description = %v, want %v", agent.description, description)
	}
	if agent.instructions != instructions {
		t.Errorf("Agent.instructions = %v, want %v", agent.instructions, instructions)
	}
	if agent.outputKey != outputKey {
		t.Errorf("Agent.outputKey = %v, want %v", agent.outputKey, outputKey)
	}
	if agent.maxIterations != maxIterations {
		t.Errorf("Agent.maxIterations = %v, want %v", agent.maxIterations, maxIterations)
	}
	if agent.inputSchema != schema {
		t.Errorf("Agent.inputSchema = %v, want %v", agent.inputSchema, schema)
	}
	if agent.outputSchema != schema {
		t.Errorf("Agent.outputSchema = %v, want %v", agent.outputSchema, schema)
	}
}

func TestAgentName(t *testing.T) {
	agent := NewAgent("test-agent")
	if agent.Name() != "test-agent" {
		t.Errorf("Agent.Name() = %v, want test-agent", agent.Name())
	}
}

func TestAgentDescription(t *testing.T) {
	agent := NewAgent("test-agent", WithDescription("Test description"))
	if agent.Description() != "Test description" {
		t.Errorf("Agent.Description() = %v, want Test description", agent.Description())
	}
}

func TestAgentBuildContext(t *testing.T) {
	agent := NewAgent("test-agent", WithModel("test-model"))
	ctx := context.Background()

	newCtx, session := agent.buildContext(ctx)

	if newCtx == ctx {
		t.Errorf("buildContext should return a new context")
	}
	if session == nil {
		t.Errorf("buildContext should return a session")
	}
}

func TestAgentBuildRequest(t *testing.T) {
	agent := NewAgent("test-agent",
		WithModel("test-model"),
		WithInstructions("Test instructions"),
	)

	ctx := context.Background()
	session := &Session{}
	ctx = NewSessionContext(ctx, session)
	prompt := &Prompt{
		Messages: []*Message{
			{
				ID:    "msg-1",
				Role:  RoleUser,
				Parts: []Part{TextPart{Text: "Hello"}},
			},
		},
	}

	req, err := agent.buildRequest(ctx, session, prompt)
	if err != nil {
		t.Errorf("buildRequest returned error: %v", err)
	}

	if req.Model != "test-model" {
		t.Errorf("ModelRequest.Model = %v, want test-model", req.Model)
	}
	if len(req.Messages) != 2 { // system + user message
		t.Errorf("ModelRequest.Messages length = %v, want 2", len(req.Messages))
	}
}

func TestAgentBuildRequestWithoutInstructions(t *testing.T) {
	agent := NewAgent("test-agent", WithModel("test-model"))

	ctx := context.Background()
	session := &Session{}
	prompt := &Prompt{
		Messages: []*Message{
			{
				ID:    "msg-1",
				Role:  RoleUser,
				Parts: []Part{TextPart{Text: "Hello"}},
			},
		},
	}

	req, err := agent.buildRequest(ctx, session, prompt)
	if err != nil {
		t.Errorf("buildRequest returned error: %v", err)
	}

	if req.Model != "test-model" {
		t.Errorf("ModelRequest.Model = %v, want test-model", req.Model)
	}
	if len(req.Messages) != 1 { // only user message
		t.Errorf("ModelRequest.Messages length = %v, want 1", len(req.Messages))
	}
}

func TestAgentStoreOutputToState(t *testing.T) {
	agent := NewAgent("test-agent", WithOutputKey("test-output"))
	session := &Session{}

	resp := &ModelResponse{
		Message: &Message{
			ID:    "msg-1",
			Role:  RoleAssistant,
			Parts: []Part{TextPart{Text: "Hello, world!"}},
		},
	}

	err := agent.storeOutputToState(session, resp)
	if err != nil {
		t.Errorf("storeOutputToState returned error: %v", err)
	}

	value, exists := session.State.Load("test-output")
	if !exists {
		t.Errorf("State should contain test-output key")
	}
	if value != "Hello, world!" {
		t.Errorf("State value = %v, want Hello, world!", value)
	}
}

func TestAgentStoreOutputToStateWithoutKey(t *testing.T) {
	agent := NewAgent("test-agent") // no output key
	session := &Session{}

	resp := &ModelResponse{
		Message: &Message{
			ID:    "msg-1",
			Role:  RoleAssistant,
			Parts: []Part{TextPart{Text: "Hello, world!"}},
		},
	}

	err := agent.storeOutputToState(session, resp)
	if err != nil {
		t.Errorf("storeOutputToState returned error: %v", err)
	}

	// Should not store anything
	count := 0
	session.State.Range(func(key string, value any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("State should be empty, got %d items", count)
	}
}

func TestAgentHandleTools(t *testing.T) {
	tool := &tools.Tool{
		Name: "test-tool",
		Handler: tools.HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
			return "processed: " + input, nil
		}),
	}

	agent := NewAgent("test-agent", WithTools(tool))

	part := ToolPart{
		ID:      "tool-1",
		Name:    "test-tool",
		Request: "test input",
	}

	result, err := agent.handleTools(context.Background(), part)
	if err != nil {
		t.Errorf("handleTools returned error: %v", err)
	}

	if result.Name != "test-tool" {
		t.Errorf("ToolPart.Name = %v, want test-tool", result.Name)
	}
	if result.Response != "processed: test input" {
		t.Errorf("ToolPart.Response = %v, want processed: test input", result.Response)
	}
}

func TestAgentHandleToolsNotFound(t *testing.T) {
	agent := NewAgent("test-agent") // no tools

	part := ToolPart{
		ID:      "tool-1",
		Name:    "unknown-tool",
		Request: "test input",
	}

	_, err := agent.handleTools(context.Background(), part)
	if err == nil {
		t.Errorf("handleTools should return error for unknown tool")
	}
	if err.Error() != "tool unknown-tool not found" {
		t.Errorf("handleTools error = %v, want tool unknown-tool not found", err)
	}
}

func TestAgentExecuteTools(t *testing.T) {
	tool := &tools.Tool{
		Name: "test-tool",
		Handler: tools.HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
			return "processed: " + input, nil
		}),
	}

	agent := NewAgent("test-agent", WithTools(tool))

	message := &Message{
		ID:   "msg-1",
		Role: RoleTool,
		Parts: []Part{
			ToolPart{
				ID:      "tool-1",
				Name:    "test-tool",
				Request: "test input",
			},
		},
	}

	result, err := agent.executeTools(context.Background(), message)
	if err != nil {
		t.Errorf("executeTools returned error: %v", err)
	}

	if result.ID != "msg-1" {
		t.Errorf("Result.ID = %v, want msg-1", result.ID)
	}
	if result.Role != RoleTool {
		t.Errorf("Result.Role = %v, want %v", result.Role, RoleTool)
	}
	if len(result.Parts) != 1 {
		t.Errorf("Result.Parts length = %v, want 1", len(result.Parts))
	}

	toolPart, ok := result.Parts[0].(ToolPart)
	if !ok {
		t.Errorf("Result.Parts[0] should be ToolPart")
	}
	if toolPart.Response != "processed: test input" {
		t.Errorf("ToolPart.Response = %v, want processed: test input", toolPart.Response)
	}
}

func TestAgentExecuteToolsWithError(t *testing.T) {
	tool := &tools.Tool{
		Name: "error-tool",
		Handler: tools.HandleFunc[string, string](func(ctx context.Context, input string) (string, error) {
			return "", errors.New("tool error")
		}),
	}

	agent := NewAgent("test-agent", WithTools(tool))

	message := &Message{
		ID:   "msg-1",
		Role: RoleTool,
		Parts: []Part{
			ToolPart{
				ID:      "tool-1",
				Name:    "error-tool",
				Request: "test input",
			},
		},
	}

	_, err := agent.executeTools(context.Background(), message)
	if err == nil {
		t.Errorf("executeTools should return error")
	}
	if err.Error() != "tool error" {
		t.Errorf("executeTools error = %v, want tool error", err)
	}
}

func TestAgentHandler(t *testing.T) {
	provider := &mockModelProvider{}
	agent := NewAgent("test-agent", WithProvider(provider))

	session := &Session{}
	req := &ModelRequest{
		Model: "test-model",
		Messages: []*Message{
			{
				ID:    "msg-1",
				Role:  RoleUser,
				Parts: []Part{TextPart{Text: "Hello"}},
			},
		},
	}

	handler := agent.handler(session, req)
	if handler == nil {
		t.Errorf("handler should not be nil")
	}

	// Test that handler implements Runnable interface
	var _ Runnable = handler
}

func TestAgentHandlerWithMaxIterations(t *testing.T) {
	provider := &mockModelProvider{
		generateFunc: func(ctx context.Context, req *ModelRequest, opts ...ModelOption) (*ModelResponse, error) {
			return &ModelResponse{
				Message: &Message{
					ID:    "msg-1",
					Role:  RoleTool, // This will cause another iteration
					Parts: []Part{TextPart{Text: "Tool response"}},
				},
			}, nil
		},
	}

	agent := NewAgent("test-agent",
		WithProvider(provider),
		WithMaxIterations(2),
	)

	session := &Session{}
	req := &ModelRequest{
		Model: "test-model",
		Messages: []*Message{
			{
				ID:    "msg-1",
				Role:  RoleUser,
				Parts: []Part{TextPart{Text: "Hello"}},
			},
		},
	}

	handler := agent.handler(session, req)

	ctx := context.Background()
	prompt := &Prompt{}

	_, err := handler.Run(ctx, prompt)
	if err == nil {
		t.Errorf("Run should return error when max iterations exceeded")
	}
	if err != ErrMaxIterationsExceeded {
		t.Errorf("Run error = %v, want %v", err, ErrMaxIterationsExceeded)
	}
}

func TestAgentOptions(t *testing.T) {
	// Test WithModel
	agent := NewAgent("test", WithModel("model"))
	if agent.model != "model" {
		t.Errorf("WithModel failed")
	}

	// Test WithDescription
	agent = NewAgent("test", WithDescription("desc"))
	if agent.description != "desc" {
		t.Errorf("WithDescription failed")
	}

	// Test WithInstructions
	agent = NewAgent("test", WithInstructions("instr"))
	if agent.instructions != "instr" {
		t.Errorf("WithInstructions failed")
	}

	// Test WithInputSchema
	schema := &jsonschema.Schema{Type: "object"}
	agent = NewAgent("test", WithInputSchema(schema))
	if agent.inputSchema != schema {
		t.Errorf("WithInputSchema failed")
	}

	// Test WithOutputSchema
	agent = NewAgent("test", WithOutputSchema(schema))
	if agent.outputSchema != schema {
		t.Errorf("WithOutputSchema failed")
	}

	// Test WithOutputKey
	agent = NewAgent("test", WithOutputKey("key"))
	if agent.outputKey != "key" {
		t.Errorf("WithOutputKey failed")
	}

	// Test WithProvider
	provider := &mockModelProvider{}
	agent = NewAgent("test", WithProvider(provider))
	if agent.provider != provider {
		t.Errorf("WithProvider failed")
	}

	// Test WithTools
	tool := &tools.Tool{Name: "test-tool"}
	agent = NewAgent("test", WithTools(tool))
	if len(agent.tools) != 1 || agent.tools[0] != tool {
		t.Errorf("WithTools failed")
	}

	// Test WithMiddleware
	middleware := func(h Runnable) Runnable { return h }
	agent = NewAgent("test", WithMiddleware(middleware))
	if agent.middleware == nil {
		t.Errorf("WithMiddleware failed")
	}

	// Test WithStateInputHandler
	inputHandler := func(ctx context.Context, prompt *Prompt, state *State) (*Prompt, error) {
		return prompt, nil
	}
	agent = NewAgent("test", WithStateInputHandler(inputHandler))
	if agent.inputHandler == nil {
		t.Errorf("WithStateInputHandler failed")
	}

	// Test WithStateOutputHandler
	outputHandler := func(ctx context.Context, output *Message, state *State) (*Message, error) {
		return output, nil
	}
	agent = NewAgent("test", WithStateOutputHandler(outputHandler))
	if agent.outputHandler == nil {
		t.Errorf("WithStateOutputHandler failed")
	}

	// Test WithMaxIterations
	agent = NewAgent("test", WithMaxIterations(5))
	if agent.maxIterations != 5 {
		t.Errorf("WithMaxIterations failed")
	}
}
