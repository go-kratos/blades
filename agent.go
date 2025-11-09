package blades

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/sync/errgroup"
)

var (
	_ Agent        = (*LLMAgent)(nil)
	_ AgentContext = (*LLMAgent)(nil)
)

// AgentOption is an option for configuring the Agent.
type AgentOption func(*LLMAgent)

// WithModel sets the model for the Agent.
func WithModel(model string) AgentOption {
	return func(a *LLMAgent) {
		a.model = model
	}
}

// WithDescription sets the description for the Agent.
func WithDescription(description string) AgentOption {
	return func(a *LLMAgent) {
		a.description = description
	}
}

// WithInstructions sets the instructions for the Agent.
func WithInstructions(instructions string) AgentOption {
	return func(a *LLMAgent) {
		a.instructions = instructions
	}
}

// WithInputSchema sets the input schema for the Agent.
func WithInputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *LLMAgent) {
		a.inputSchema = schema
	}
}

// WithOutputSchema sets the output schema for the Agent.
func WithOutputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *LLMAgent) {
		a.outputSchema = schema
	}
}

// WithOutputKey sets the output key for storing the Agent's output in the session state.
func WithOutputKey(key string) AgentOption {
	return func(a *LLMAgent) {
		a.outputKey = key
	}
}

// WithProvider sets the model provider for the Agent.
func WithProvider(provider ModelProvider) AgentOption {
	return func(a *LLMAgent) {
		a.provider = provider
	}
}

// WithTools sets the tools for the Agent.
func WithTools(tools ...*tools.Tool) AgentOption {
	return func(a *LLMAgent) {
		a.tools = tools
	}
}

// WithToolsResolver sets a tools resolver for the Agent.
// The resolver can dynamically provide tools from various sources (e.g., MCP servers, plugins).
// Tools are resolved lazily on first use.
func WithToolsResolver(r tools.Resolver) AgentOption {
	return func(a *LLMAgent) {
		a.toolsResolver = r
	}
}

// WithMiddleware sets the middleware for the Agent.
func WithMiddleware(ms ...Middleware) AgentOption {
	return func(a *LLMAgent) {
		a.middlewares = ms
	}
}

// WithMaxIterations sets the maximum number of iterations for the Agent.
// By default, it is set to 10.
func WithMaxIterations(n int) AgentOption {
	return func(a *LLMAgent) {
		a.maxIterations = n
	}
}

// LLMAgent is a struct that represents an AI agent.
type LLMAgent struct {
	name          string
	model         string
	description   string
	instructions  string
	outputKey     string
	maxIterations int
	inputSchema   *jsonschema.Schema
	outputSchema  *jsonschema.Schema
	middlewares   []Middleware
	provider      ModelProvider
	tools         []*tools.Tool
	toolsResolver tools.Resolver // Optional resolver for dynamic tools (e.g., MCP servers)
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...AgentOption) *LLMAgent {
	a := &LLMAgent{
		name:          name,
		maxIterations: 10,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the name of the Agent.
func (a *LLMAgent) Name() string {
	return a.name
}

// Model returns the model of the Agent.
func (a *LLMAgent) Model() string {
	return a.model
}

// Description returns the description of the Agent.
func (a *LLMAgent) Description() string {
	return a.description
}

// Instructions returns the instructions of the Agent.
func (a *LLMAgent) Instructions() string {
	return a.instructions
}

// resolveTools combines static tools with dynamically resolved tools.
func (a *LLMAgent) resolveTools(ctx context.Context) ([]*tools.Tool, error) {
	tools := make([]*tools.Tool, 0, len(a.tools))
	if len(a.tools) > 0 {
		tools = append(tools, a.tools...)
	}
	if a.toolsResolver != nil {
		resolved, err := a.toolsResolver.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		tools = append(tools, resolved...)
	}
	return tools, nil
}

// buildRequest builds the request for the Agent by combining system instructions and user messages.
func (a *LLMAgent) buildRequest(ctx context.Context, invocation *Invocation) (*ModelRequest, error) {
	tools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	req := ModelRequest{
		Model:        a.model,
		Tools:        tools,
		InputSchema:  a.inputSchema,
		OutputSchema: a.outputSchema,
	}
	// system messages
	if a.instructions != "" {
		systemMessage, err := NewTemplateMessage(RoleSystem, a.instructions, map[string]any(invocation.Session.State()))
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, systemMessage)
	}
	// user messages
	if invocation.Message != nil {
		req.Messages = append(req.Messages, invocation.Message)
	}
	return &req, nil
}

// Run runs the agent with the given prompt and options, returning a streamable response.
func (a *LLMAgent) Run(ctx context.Context, invocation *Invocation) Sequence[*Message, error] {
	return func(yield func(*Message, error) bool) {
		ctx := NewAgentContext(ctx, a)
		// find resume message
		if message, ok := a.findResumeMessage(ctx, invocation); ok {
			yield(message, nil)
			return
		}
		req, err := a.buildRequest(ctx, invocation)
		if err != nil {
			yield(nil, err)
			return
		}
		stream := a.handle(ctx, invocation, req)
		for m, err := range stream {
			if !yield(m, err) {
				break
			}
		}
	}
}

func (a *LLMAgent) findResumeMessage(ctx context.Context, invocation *Invocation) (*Message, bool) {
	if !invocation.Resumable {
		return nil, false
	}
	for _, m := range invocation.Session.History() {
		if m.InvocationID == invocation.ID &&
			m.Author == a.name && m.Role == RoleAssistant && m.Status == StatusCompleted {
			return m, true
		}
	}
	return nil, false
}

// storeSession stores the agent's output to session state (if outputKey is defined) and appends messages to session history.
func (a *LLMAgent) storeSession(ctx context.Context, invocation *Invocation, toolMessages []*Message, assistantMessage *Message) error {
	if a.outputKey != "" {
		if a.outputSchema != nil {
			value, err := ParseMessageState(assistantMessage, a.outputSchema)
			if err != nil {
				return err
			}
			invocation.Session.PutState(a.outputKey, value)
		} else {
			invocation.Session.PutState(a.outputKey, assistantMessage.Text())
		}
	}
	stores := make([]*Message, 0, len(toolMessages)+2)
	stores = append(stores, setMessageContext("user", invocation.ID, invocation.Message)...)
	stores = append(stores, setMessageContext(a.name, invocation.ID, toolMessages...)...)
	stores = append(stores, setMessageContext(a.name, invocation.ID, assistantMessage)...)
	return invocation.Session.Append(ctx, stores)
}

func (a *LLMAgent) handleTools(ctx context.Context, part ToolPart) (ToolPart, error) {
	tools, err := a.resolveTools(ctx)
	if err != nil {
		return part, err
	}
	// Search through all available tools (static + resolved)
	for _, tool := range tools {
		if tool.Name == part.Name {
			response, err := tool.Handler.Handle(ctx, part.Request)
			if err != nil {
				return part, err
			}
			part.Response = response
			return part, nil
		}
	}
	return part, fmt.Errorf("tool %s not found", part.Name)
}

// executeTools executes the tools specified in the tool parts.
func (a *LLMAgent) executeTools(ctx context.Context, message *Message) (*Message, error) {
	toolMessage := &Message{ID: message.ID, Role: message.Role, Parts: message.Parts}
	eg, ctx := errgroup.WithContext(ctx)
	for i, part := range message.Parts {
		switch v := any(part).(type) {
		case ToolPart:
			eg.Go(func() error {
				part, err := a.handleTools(ctx, v)
				if err != nil {
					return err
				}
				toolMessage.Parts[i] = part
				return nil
			})
		}
	}
	return toolMessage, eg.Wait()
}

// handle constructs the default handlers for Run and Stream using the provider.
func (a *LLMAgent) handle(ctx context.Context, invocation *Invocation, req *ModelRequest) Sequence[*Message, error] {
	return func(yield func(*Message, error) bool) {
		var (
			err           error
			toolMessages  []*Message
			finalResponse *ModelResponse
		)
		for i := 0; i < a.maxIterations; i++ {
			if invocation.Stream {
				finalResponse, err = a.provider.Generate(ctx, req, invocation.ModelOptions...)
				if err != nil {
					yield(nil, err)
					return
				}
			} else {
				streaming := a.provider.NewStreaming(ctx, req, invocation.ModelOptions...)
				for res, err := range streaming {
					if err != nil {
						yield(nil, err)
						return
					}
					if res.Message.Status == StatusCompleted {
						finalResponse = res
					} else {
						if !yield(res.Message, nil) {
							return // early termination
						}
					}
				}
			}
			if finalResponse == nil {
				yield(nil, ErrNoFinalResponse)
				return
			}
			if finalResponse.Message.Role == RoleTool {
				toolMessage, err := a.executeTools(ctx, finalResponse.Message)
				if err != nil {
					yield(nil, err)
					return
				}
				req.Messages = append(req.Messages, toolMessage)
				toolMessages = append(toolMessages, toolMessage)
				continue // continue to the next iteration
			}
			if err := a.storeSession(ctx, invocation, toolMessages, finalResponse.Message); err != nil {
				yield(nil, err)
				return
			}
			yield(finalResponse.Message, nil)
			return
		}
		yield(nil, ErrMaxIterationsExceeded)
	}
}
