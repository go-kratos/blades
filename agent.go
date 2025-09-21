package blades

import (
	"context"
)

var (
	_ Runner = (*Agent)(nil)
)

// Option is an option for configuring the Agent.
type Option func(*Agent)

// WithModel sets the model for the Agent.
func WithModel(model string) Option {
	return func(a *Agent) {
		a.model = model
	}
}

// WithInstructions sets the instructions for the Agent.
func WithInstructions(instructions string) Option {
	return func(a *Agent) {
		a.instructions = instructions
	}
}

// WithProvider sets the model provider for the Agent.
func WithProvider(provider ModelProvider) Option {
	return func(a *Agent) {
		a.provider = provider
	}
}

// WithTools sets the tools for the Agent.
func WithTools(tools ...*Tool) Option {
	return func(a *Agent) {
		a.tools = tools
	}
}

// WithMemory sets the memory for the Agent.
func WithMemory(m Memory) Option {
	return func(a *Agent) {
		a.memory = m
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
	instructions string
	middleware   Middleware
	provider     ModelProvider
	memory       Memory
	tools        []*Tool
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...Option) *Agent {
	a := &Agent{
		name: name,
		middleware: func(next Runner) Runner {
			return next // nop middleware
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// buildRequest builds the request for the Agent by combining system instructions and user messages.
func (a *Agent) buildRequest(ctx context.Context, prompt *Prompt) (*ModelRequest, error) {
	req := ModelRequest{Model: a.model, Tools: a.tools}
	// system messages
	if a.instructions != "" {
		req.Messages = append(req.Messages, SystemMessage(a.instructions))
	}
	// memory messages
	if a.memory != nil {
		history, err := a.memory.ListMessages(ctx, prompt.ConversationID)
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, history...)
	}
	// user messages
	if len(prompt.Messages) > 0 {
		req.Messages = append(req.Messages, prompt.Messages...)
	}
	return &req, nil
}

func (a *Agent) addMemory(ctx context.Context, prompt *Prompt, res *ModelResponse) error {
	if a.memory != nil {
		messages := make([]*Message, 0, len(prompt.Messages)+1)
		messages = append(messages, prompt.Messages...)
		messages = append(messages, res.Message)
		if err := a.memory.AddMessages(ctx, prompt.ConversationID, messages); err != nil {
			return err
		}
	}
	return nil
}

// Run runs the agent with the given prompt and options, returning the response message.
func (a *Agent) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Generation, error) {
	req, err := a.buildRequest(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return a.middleware(RunnerFunc{
		run: func(ctx context.Context, p *Prompt, opts ...ModelOption) (*Generation, error) {
			res, err := a.provider.Generate(ctx, req, opts...)
			if err != nil {
				return nil, err
			}
			if err := a.addMemory(ctx, p, res); err != nil {
				return nil, err
			}
			return &Generation{res.Message}, nil
		},
	}).Run(ctx, prompt, opts...)
}

// RunStream runs the agent with the given prompt and options, returning a streamable response.
func (a *Agent) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamer[*Generation], error) {
	req, err := a.buildRequest(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return a.middleware(RunnerFunc{
		runStream: func(ctx context.Context, p *Prompt, opts ...ModelOption) (Streamer[*Generation], error) {
			stream, err := a.provider.NewStream(ctx, req, opts...)
			if err != nil {
				return nil, err
			}
			return NewMappedStream[*ModelResponse, *Generation](stream, func(m *ModelResponse) (*Generation, error) {
				if err := a.addMemory(ctx, p, m); err != nil {
					return nil, err
				}
				return &Generation{m.Message}, nil
			}), nil
		},
	}).RunStream(ctx, prompt, opts...)
}
