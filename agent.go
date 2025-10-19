package blades

import (
	"context"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

var (
	_ Runnable[*Prompt, *Message, ModelOption] = (*Agent)(nil)
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

// WithMiddleware sets the middleware for the Agent.
func WithMiddleware(m Middleware) Option {
	return func(a *Agent) {
		a.middleware = m
	}
}

// Agent is a struct that represents an AI agent.
type Agent struct {
	name         string
	model        string
	description  string
	instructions string
	outputKey    string
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	middleware   Middleware
	provider     ModelProvider
	tools        []*tools.Tool
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...Option) *Agent {
	a := &Agent{
		name:       name,
		middleware: func(h Handler) Handler { return h },
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

// buildContext builds the context for the Agent by embedding the AgentContext.
func (a *Agent) buildContext(ctx context.Context) (context.Context, *Session) {
	session, ctx := EnsureSession(ctx)
	return NewContext(ctx, &AgentContext{
		Name:         a.name,
		Model:        a.model,
		Description:  a.description,
		Instructions: a.instructions,
	}), session
}

// buildRequest builds the request for the Agent by combining system instructions and user messages.
func (a *Agent) buildRequest(ctx context.Context, prompt *Prompt) (*ModelRequest, error) {
	req := ModelRequest{
		Model:        a.model,
		Tools:        a.tools,
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

func (a *Agent) storeOutputToState(session *Session, res *ModelResponse) error {
	if a.outputKey == "" {
		return nil
	}
	if a.outputSchema != nil {
		value, err := parseMessageState(a.outputSchema, res.Message)
		if err != nil {
			return err
		}
		session.State.Store(a.outputKey, value)
	} else {
		session.State.Store(a.outputKey, res.Message.Text())
	}
	return nil
}

// Run runs the agent with the given prompt and options, returning the response message.
func (a *Agent) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
	ctx, session := a.buildContext(ctx)
	req, err := a.buildRequest(ctx, prompt)
	if err != nil {
		return nil, err
	}
	handler := a.middleware(a.handler(session, req))
	return handler.Run(ctx, prompt, opts...)
}

// RunStream runs the agent with the given prompt and options, returning a streamable response.
func (a *Agent) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
	ctx, session := a.buildContext(ctx)
	req, err := a.buildRequest(ctx, prompt)
	if err != nil {
		return nil, err
	}
	handler := a.middleware(a.handler(session, req))
	return handler.Stream(ctx, prompt, opts...)
}

// handler constructs the default handlers for Run and Stream using the provider.
func (a *Agent) handler(session *Session, req *ModelRequest) Handler {
	return Handler{
		Run: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
			res, err := a.provider.Generate(ctx, req, opts...)
			if err != nil {
				return nil, err
			}
			if err := a.storeOutputToState(session, res); err != nil {
				return nil, err
			}
			session.Record(a.name, prompt, res.Message)
			return res.Message, nil
		},
		Stream: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
			stream, err := a.provider.NewStream(ctx, req, opts...)
			if err != nil {
				return nil, err
			}
			return NewMappedStream[*ModelResponse, *Message](stream, func(res *ModelResponse) (*Message, error) {
				if res.Message.Status == StatusCompleted {
					if err := a.storeOutputToState(session, res); err != nil {
						return nil, err
					}
					session.Record(a.name, prompt, res.Message)
				}
				return res.Message, nil
			}), nil
		},
	}
}
