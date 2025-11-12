package blades

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/sync/errgroup"
)

var (
	_ AgentContext = (*agent)(nil)
)

// AgentOption is an option for configuring the Agent.
type AgentOption func(*agent)

// WithModel sets the model for the Agent.
func WithModel(model string) AgentOption {
	return func(a *agent) {
		a.model = model
	}
}

// WithDescription sets the description for the Agent.
func WithDescription(description string) AgentOption {
	return func(a *agent) {
		a.description = description
	}
}

// WithInstructions sets the instructions for the Agent.
func WithInstructions(instructions string) AgentOption {
	return func(a *agent) {
		a.instructions = instructions
	}
}

// WithInputSchema sets the input schema for the Agent.
func WithInputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *agent) {
		a.inputSchema = schema
	}
}

// WithOutputSchema sets the output schema for the Agent.
func WithOutputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *agent) {
		a.outputSchema = schema
	}
}

// WithProvider sets the model provider for the Agent.
func WithProvider(provider ModelProvider) AgentOption {
	return func(a *agent) {
		a.provider = provider
	}
}

// WithTools sets the tools for the Agent.
func WithTools(tools ...tools.Tool) AgentOption {
	return func(a *agent) {
		a.tools = tools
	}
}

// WithToolsResolver sets a tools resolver for the Agent.
// The resolver can dynamically provide tools from various sources (e.g., MCP servers, plugins).
// Tools are resolved lazily on first use.
func WithToolsResolver(r tools.Resolver) AgentOption {
	return func(a *agent) {
		a.toolsResolver = r
	}
}

// WithMiddleware sets the middleware for the Agent.
func WithMiddleware(ms ...Middleware) AgentOption {
	return func(a *agent) {
		a.middlewares = ms
	}
}

// WithMaxIterations sets the maximum number of iterations for the Agent.
// By default, it is set to 10.
func WithMaxIterations(n int) AgentOption {
	return func(a *agent) {
		a.maxIterations = n
	}
}

// agent is a struct that represents an AI agent.
type agent struct {
	name          string
	model         string
	description   string
	instructions  string
	maxIterations int
	inputSchema   *jsonschema.Schema
	outputSchema  *jsonschema.Schema
	provider      ModelProvider
	middlewares   []Middleware
	tools         []tools.Tool
	toolsResolver tools.Resolver // Optional resolver for dynamic tools (e.g., MCP servers)
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...AgentOption) (Agent, error) {
	a := &agent{
		name:          name,
		maxIterations: 10,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, ErrModelProviderRequired
	}
	return a, nil
}

// Name returns the name of the Agent.
func (a *agent) Name() string {
	return a.name
}

// Model returns the model of the Agent.
func (a *agent) Model() string {
	return a.model
}

// Tools returns the tools of the Agent.
func (a *agent) Tools() []tools.Tool {
	return a.tools
}

// Description returns the description of the Agent.
func (a *agent) Description() string {
	return a.description
}

// Instructions returns the instructions of the Agent.
func (a *agent) Instructions() string {
	return a.instructions
}

// InputSchema returns the input schema of the Agent.
func (a *agent) InputSchema() *jsonschema.Schema {
	return a.inputSchema
}

// OutputSchema returns the output schema of the Agent.
func (a *agent) OutputSchema() *jsonschema.Schema {
	return a.outputSchema
}

// resolveTools combines static tools with dynamically resolved tools.
func (a *agent) resolveTools(ctx context.Context) ([]tools.Tool, error) {
	tools := make([]tools.Tool, 0, len(a.tools))
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
func (a *agent) buildRequest(ctx context.Context, invocation *Invocation) (*ModelRequest, error) {
	resolvedTools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	req := ModelRequest{
		Model:        a.model,
		Tools:        resolvedTools,
		InputSchema:  a.inputSchema,
		OutputSchema: a.outputSchema,
	}
	// Process system instructions with template if session state is available
	if a.instructions != "" {
		var (
			state State
			buf   strings.Builder
		)
		if invocation.Session != nil {
			state = invocation.Session.State()
			t, err := template.New("instructions").Parse(a.instructions)
			if err != nil {
				return nil, err
			}
			if err := t.Execute(&buf, state); err != nil {
				return nil, err
			}
		} else {
			buf.WriteString(a.instructions)
		}
		req.Instruction = SystemMessage(buf.String())
	}
	// Append history and the current message
	if len(invocation.History) > 0 {
		req.Messages = append(req.Messages, invocation.History...)
	}
	// Append the current user message
	if invocation.Message != nil {
		req.Messages = append(req.Messages, invocation.Message)
	}
	return &req, nil
}

// Run runs the agent with the given prompt and options, returning a streamable response.
func (a *agent) Run(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		ctx = NewAgentContext(ctx, a)
		// If resumable and a completed message exists, return it directly.
		if resumeMessage, ok := a.findResumeMessage(ctx, invocation); ok {
			yield(resumeMessage, nil)
			return
		}
		handler := Handler(HandleFunc(func(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
			req, err := a.buildRequest(ctx, invocation)
			if err != nil {
				return func(yield func(*Message, error) bool) {
					yield(nil, err)
				}
			}
			return a.handle(ctx, invocation, req)
		}))
		if len(a.middlewares) > 0 {
			handler = ChainMiddlewares(a.middlewares...)(handler)
		}
		stream := handler.Handle(ctx, invocation)
		for m, err := range stream {
			if !yield(m, err) {
				break
			}
		}
	}
}

func (a *agent) findResumeMessage(ctx context.Context, invocation *Invocation) (*Message, bool) {
	if !invocation.Resumable || invocation.Session == nil {
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

// storeSession stores the conversation messages in the session.
func (a *agent) storeSession(ctx context.Context, invocation *Invocation, toolMessages []*Message, assistantMessage *Message) error {
	if invocation.Session == nil {
		return nil
	}
	stores := make([]*Message, 0, len(toolMessages)+2)
	stores = append(stores, setMessageContext("user", invocation.ID, invocation.Message)...)
	stores = append(stores, setMessageContext(a.name, invocation.ID, toolMessages...)...)
	stores = append(stores, setMessageContext(a.name, invocation.ID, assistantMessage)...)
	return invocation.Session.Append(ctx, stores)
}

func (a *agent) handleTools(ctx context.Context, part ToolPart) (ToolPart, error) {
	tools, err := a.resolveTools(ctx)
	if err != nil {
		return part, err
	}
	// Search through all available tools (static + resolved)
	for _, tool := range tools {
		if tool.Name() == part.Name {
			response, err := tool.Handle(ctx, part.Request)
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
func (a *agent) executeTools(ctx context.Context, message *Message) (*Message, error) {
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
func (a *agent) handle(ctx context.Context, invocation *Invocation, req *ModelRequest) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		var (
			err           error
			toolMessages  []*Message
			finalResponse *ModelResponse
		)
		for i := 0; i < a.maxIterations; i++ {
			if !invocation.Streamable {
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
