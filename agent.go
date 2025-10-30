package blades

import (
	"context"
	"fmt"

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
}

// NewAgent creates a new Agent with the given name and options.
func NewAgent(name string, opts ...Option) *Agent {
	a := &Agent{
		name:          name,
		maxIterations: 10,
		inputHandler: func(ctx context.Context, prompt *Prompt, state *State) (*Prompt, error) {
			return prompt, nil
		},
		outputHandler: func(ctx context.Context, output *Message, state *State) (*Message, error) {
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
func (a *Agent) buildRequest(ctx context.Context, session *Session, prompt *Prompt) (*ModelRequest, error) {
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

// Run runs the agent with the given prompt and options, returning the response message.
func (a *Agent) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
	ctx, session := a.buildContext(ctx)
	input, err := a.inputHandler(ctx, prompt, &session.State)
	if err != nil {
		return nil, err
	}
	req, err := a.buildRequest(ctx, session, input)
	if err != nil {
		return nil, err
	}
	handler := a.handler(session, req)
	return handler.Run(ctx, prompt, opts...)
}

// RunStream runs the agent with the given prompt and options, returning a streamable response.
func (a *Agent) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
	ctx, session := a.buildContext(ctx)
	input, err := a.inputHandler(ctx, prompt, &session.State)
	if err != nil {
		return nil, err
	}
	req, err := a.buildRequest(ctx, session, input)
	if err != nil {
		return nil, err
	}
	handler := a.handler(session, req)
	return handler.RunStream(ctx, prompt, opts...)
}

// storeOutputToState stores the output of the Agent to the session state if an output key is defined.
func (a *Agent) storeOutputToState(session *Session, res *ModelResponse) error {
	if a.outputKey == "" {
		return nil
	}
	if a.outputSchema != nil {
		value, err := ParseMessageState(res.Message, a.outputSchema)
		if err != nil {
			return err
		}
		session.PutState(a.outputKey, value)
	} else {
		session.PutState(a.outputKey, res.Message.Text())
	}
	return nil
}

func (a *Agent) handleTools(ctx context.Context, part ToolPart) (ToolPart, error) {
	for _, tool := range a.tools {
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
func (a *Agent) handler(session *Session, req *ModelRequest) Runnable {
	handler := Runnable(&HandleFunc{
		Handle: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
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
					continue // continue to the next iteration
				}
				if err := a.storeOutputToState(session, res); err != nil {
					return nil, err
				}
				session.Record(req.Messages, res.Message)
				return a.outputHandler(ctx, res.Message, &session.State)
			}
			return nil, ErrMaxIterationsExceeded
		},
		HandleStream: func(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
			pipe := NewStreamPipe[*Message]()
			pipe.Go(func() error {
				for i := 0; i < a.maxIterations; i++ {
					stream, err := a.provider.NewStream(ctx, req, opts...)
					if err != nil {
						return err
					}
					var finalResponse *ModelResponse
					for stream.Next() {
						chunk, err := stream.Current()
						if err != nil {
							return err
						}
						if chunk.Message.Status == StatusCompleted {
							finalResponse = chunk
						} else {
							pipe.Send(chunk.Message)
						}
					}
					if finalResponse == nil {
						return ErrMissingFinalResponse
					}
					if finalResponse.Message.Role == RoleTool {
						toolMessage, err := a.executeTools(ctx, finalResponse.Message)
						if err != nil {
							return err
						}
						req.Messages = append(req.Messages, toolMessage)
						continue // continue to the next iteration
					}
					if err := a.storeOutputToState(session, finalResponse); err != nil {
						return err
					}
					session.Record(req.Messages, finalResponse.Message)
					// handle the final response before sending
					finalResponse.Message, err = a.outputHandler(ctx, finalResponse.Message, &session.State)
					if err != nil {
						return err
					}
					pipe.Send(finalResponse.Message)
					return nil
				}
				return ErrMaxIterationsExceeded
			})
			return pipe, nil
		},
	})
	if len(a.middlewares) > 0 {
		handler = ChainMiddlewares(a.middlewares...)(handler)
	}
	return handler
}
