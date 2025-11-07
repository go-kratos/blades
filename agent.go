package blades

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/stream"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/sync/errgroup"
)

var (
	_ Runnable = (*Agent)(nil)
)

// Option is an option for configuring the Agent.
type Option func(*Agent)

// WithModel sets the model for the Agent.
func WithModel(model string) Option {
	return func(a *Agent) {
		a.model = model
	}
}

// WithDescription sets the description for the Agent.
func WithDescription(description string) Option {
	return func(a *Agent) {
		a.description = description
	}
}

// WithInstructions sets the instructions for the Agent.
func WithInstructions(instructions string) Option {
	return func(a *Agent) {
		a.instructions = instructions
	}
}

// WithInputSchema sets the input schema for the Agent.
func WithInputSchema(schema *jsonschema.Schema) Option {
	return func(a *Agent) {
		a.inputSchema = schema
	}
}

// WithOutputSchema sets the output schema for the Agent.
func WithOutputSchema(schema *jsonschema.Schema) Option {
	return func(a *Agent) {
		a.outputSchema = schema
	}
}

// WithOutputKey sets the output key for storing the Agent's output in the session state.
func WithOutputKey(key string) Option {
	return func(a *Agent) {
		a.outputKey = key
	}
}

// WithProvider sets the model provider for the Agent.
func WithProvider(provider ModelProvider) Option {
	return func(a *Agent) {
		a.provider = provider
	}
}

// WithTools sets the tools for the Agent.
func WithTools(tools ...*tools.Tool) Option {
	return func(a *Agent) {
		a.tools = tools
	}
}

// WithToolsResolver sets a tools resolver for the Agent.
// The resolver can dynamically provide tools from various sources (e.g., MCP servers, plugins).
// Tools are resolved lazily on first use.
func WithToolsResolver(r tools.Resolver) Option {
	return func(a *Agent) {
		a.toolsResolver = r
	}
}

// WithMiddleware sets the middleware for the Agent.
func WithMiddleware(ms ...Middleware) Option {
	return func(a *Agent) {
		a.middlewares = ms
	}
}

// WithStateInputHandler sets the state input handler for the Agent.
func WithStateInputHandler(h StateInputHandler) Option {
	return func(a *Agent) {
		a.inputHandler = h
	}
}

// WithStateOutputHandler sets the state output handler for the Agent.
func WithStateOutputHandler(h StateOutputHandler) Option {
	return func(a *Agent) {
		a.outputHandler = h
	}
}

// WithMaxIterations sets the maximum number of iterations for the Agent.
// By default, it is set to 10.
func WithMaxIterations(n int) Option {
	return func(a *Agent) {
		a.maxIterations = n
	}
}

// Agent is a struct that represents an AI agent.
type Agent struct {
	name          string
	model         string
	description   string
	instructions  string
	outputKey     string
	maxIterations int
	inputSchema   *jsonschema.Schema
	outputSchema  *jsonschema.Schema
	inputHandler  StateInputHandler
	outputHandler StateOutputHandler
	middlewares   []Middleware
	provider      ModelProvider
	tools         []*tools.Tool
	toolsResolver tools.Resolver // Optional resolver for dynamic tools (e.g., MCP servers)
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...Option) *Agent {
	a := &Agent{
		name:          name,
		maxIterations: 10,
		inputHandler: func(ctx context.Context, prompt *Prompt, state State) (*Prompt, error) {
			return prompt, nil
		},
		outputHandler: func(ctx context.Context, output *Message, state State) (*Message, error) {
			return output, nil
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the name of the Agent.
func (a *Agent) Name() string {
	return a.name
}

// Description returns the description of the Agent.
func (a *Agent) Description() string {
	return a.description
}

// buildInvocationContext builds the context for the Agent by embedding the AgentContext.
func (a *Agent) buildInvocationContext(ctx context.Context) (context.Context, *InvocationContext) {
	invocation, ctx := EnsureInvocationContext(ctx)
	return NewContext(ctx, &AgentContext{
		Name:         a.name,
		Model:        a.model,
		Description:  a.description,
		Instructions: a.instructions,
	}), invocation
}

// resolveTools combines static tools with dynamically resolved tools.
func (a *Agent) resolveTools(ctx context.Context) ([]*tools.Tool, error) {
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
func (a *Agent) buildRequest(ctx context.Context, prompt *Prompt) (*ModelRequest, error) {
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
		system, err := NewPromptTemplate().System(a.instructions).BuildContext(ctx)
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, system.Messages...)
	}
	// user messages
	if len(prompt.Messages) > 0 {
		req.Messages = append(req.Messages, prompt.Messages...)
	}
	return &req, nil
}

// Run runs the agent with the given prompt and options, returning the response message.
func (a *Agent) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
	ctx, invocation := a.buildInvocationContext(ctx)
	input, err := a.inputHandler(ctx, prompt, invocation.Session.State())
	if err != nil {
		return nil, err
	}
	req, err := a.buildRequest(ctx, input)
	if err != nil {
		return nil, err
	}
	handler := a.handler(invocation, req)
	return handler.Run(ctx, prompt, opts...)
}

// RunStream runs the agent with the given prompt and options, returning a streamable response.
func (a *Agent) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (stream.Streamable[*Message], error) {
	ctx, invocation := a.buildInvocationContext(ctx)
	input, err := a.inputHandler(ctx, prompt, invocation.Session.State())
	if err != nil {
		return nil, err
	}
	req, err := a.buildRequest(ctx, input)
	if err != nil {
		return nil, err
	}
	handler := a.handler(invocation, req)
	return handler.RunStream(ctx, prompt, opts...)
}

func (a *Agent) findResumeMessage(ctx context.Context, invocation *InvocationContext) (*Message, bool) {
	if !invocation.Resumable {
		return nil, false
	}
	for _, m := range invocation.Session.History() {
		if m.InvocationID == invocation.InvocationID &&
			m.Author == a.name && m.Role == RoleAssistant && m.Status == StatusCompleted {
			return m, true
		}
	}
	return nil, false
}

// storeSession stores the agent's output to session state (if outputKey is defined) and appends messages to session history.
func (a *Agent) storeSession(ctx context.Context, invocation *InvocationContext, userMessages, toolMessages []*Message, assistantMessage *Message) error {
	state := State{}
	if a.outputKey != "" {
		if a.outputSchema != nil {
			value, err := ParseMessageState(assistantMessage, a.outputSchema)
			if err != nil {
				return err
			}
			state[a.outputKey] = value
		} else {
			state[a.outputKey] = assistantMessage.Text()
		}
	}
	stores := make([]*Message, 0, len(userMessages)+len(toolMessages)+1)
	stores = append(stores, setMessageContext("user", invocation.InvocationID, userMessages...)...)
	stores = append(stores, setMessageContext(a.name, invocation.InvocationID, toolMessages...)...)
	stores = append(stores, setMessageContext(a.name, invocation.InvocationID, assistantMessage)...)
	return invocation.Session.Append(ctx, state, stores)
}

func (a *Agent) handleTools(ctx context.Context, part ToolPart) (ToolPart, error) {
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
func (a *Agent) executeTools(ctx context.Context, message *Message) (*Message, error) {
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

// handler constructs the default handlers for Run and Stream using the provider.
func (a *Agent) handler(invocation *InvocationContext, req *ModelRequest) Runnable {
	handler := Runnable(&HandleFunc{
		Handle: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
			// find resume message
			if message, ok := a.findResumeMessage(ctx, invocation); ok {
				return message, nil
			}
			var toolMessages []*Message
			for i := 0; i < a.maxIterations; i++ {
				res, err := a.provider.Generate(ctx, req, opts...)
				if err != nil {
					return nil, err
				}
				if res.Message.Role == RoleTool {
					toolMessage, err := a.executeTools(ctx, res.Message)
					if err != nil {
						return nil, err
					}
					req.Messages = append(req.Messages, toolMessage)
					toolMessages = append(toolMessages, toolMessage)
					continue // continue to the next iteration
				}
				output, err := a.outputHandler(ctx, res.Message, invocation.Session.State())
				if err != nil {
					return nil, err
				}
				if err := a.storeSession(ctx, invocation, prompt.Messages, toolMessages, output); err != nil {
					return nil, err
				}
				return output, nil
			}
			return nil, ErrMaxIterationsExceeded
		},
		HandleStream: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (stream.Streamable[*Message], error) {
			return stream.Go(func(yield func(*Message, error) bool) {
				// find resume message
				if message, ok := a.findResumeMessage(ctx, invocation); ok {
					yield(message, nil)
					return
				}
				var toolMessages []*Message
				for i := 0; i < a.maxIterations; i++ {
					events, err := a.provider.NewStream(ctx, req, opts...)
					if err != nil {
						yield(nil, err)
						return
					}
					var finalResponse *ModelResponse
					for res, err := range events {
						if err != nil {
							yield(nil, err)
							return
						}
						if res.Message.Status == StatusCompleted {
							finalResponse = res
						} else {
							yield(res.Message, nil)
						}
					}
					if finalResponse == nil {
						yield(nil, ErrMissingFinalResponse)
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
					// handle the final response before sending
					finalResponse.Message, err = a.outputHandler(ctx, finalResponse.Message, invocation.Session.State())
					if err != nil {
						yield(nil, err)
						return
					}
					if err := a.storeSession(ctx, invocation, prompt.Messages, toolMessages, finalResponse.Message); err != nil {
						yield(nil, err)
						return
					}
					yield(finalResponse.Message, nil)
					return
				}
				yield(nil, ErrMaxIterationsExceeded)
			}), nil
		},
	})
	if len(a.middlewares) > 0 {
		handler = ChainMiddlewares(a.middlewares...)(handler)
	}
	return handler
}
