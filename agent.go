package blades

import (
	"context"
	"fmt"
	"html/template"
	"maps"
	"strings"
	"sync"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/sync/errgroup"
)

// InstructionProvider is a function type that generates instructions based on the given context.
type InstructionProvider func(ctx context.Context) (string, error)

// AgentOption is an option for configuring the Agent.
type AgentOption func(*agent)

// WithModel sets the model provider for the Agent.
func WithModel(model ModelProvider) AgentOption {
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

// WithOutputKey sets the output key for storing the Agent's output in the session state.
func WithOutputKey(key string) AgentOption {
	return func(a *agent) {
		a.outputKey = key
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

// WithInstructionProvider sets a dynamic instruction provider for the Agent.
func WithInstructionProvider(p InstructionProvider) AgentOption {
	return func(a *agent) {
		a.instructionProvider = p
	}
}

// agent is a struct that represents an AI agent.
type agent struct {
	name                string
	description         string
	instructions        string
	instructionProvider InstructionProvider
	outputKey           string
	maxIterations       int
	model               ModelProvider
	inputSchema         *jsonschema.Schema
	outputSchema        *jsonschema.Schema
	middlewares         []Middleware
	tools               []tools.Tool
	toolsResolver       tools.Resolver // Optional resolver for dynamic tools (e.g., MCP servers)
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
	if a.model == nil {
		return nil, ErrModelProviderRequired
	}
	return a, nil
}

// Name returns the name of the Agent.
func (a *agent) Name() string {
	return a.name
}

// Description returns the description of the Agent.
func (a *agent) Description() string {
	return a.description
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

// prepareInvocation prepares the invocation by resolving tools and applying instructions.
func (a *agent) prepareInvocation(ctx context.Context, invocation *Invocation) error {
	resolvedTools, err := a.resolveTools(ctx)
	if err != nil {
		return err
	}
	invocation.Model = a.model.Name()
	invocation.Tools = append(invocation.Tools, resolvedTools...)
	// order of precedence: static instruction > instruction provider > invocation instruction
	if a.instructionProvider != nil {
		instruction, err := a.instructionProvider(ctx)
		if err != nil {
			return err
		}
		invocation.Instruction = MergeParts(SystemMessage(instruction), invocation.Instruction)
	}
	if a.instructions != "" {
		if invocation.Session != nil {
			var buf strings.Builder
			t, err := template.New("instructions").Parse(a.instructions)
			if err != nil {
				return err
			}
			if err := t.Execute(&buf, invocation.Session.State()); err != nil {
				return err
			}
			invocation.Instruction = MergeParts(SystemMessage(buf.String()), invocation.Instruction)
		} else {
			invocation.Instruction = MergeParts(SystemMessage(a.instructions), invocation.Instruction)
		}
	}
	return nil
}

// Run runs the agent with the given prompt and options, returning a streamable response.
func (a *agent) Run(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		// If resumable and a completed message exists, return it directly.
		if resumeMessage, ok := a.findResumeMessage(ctx, invocation); ok {
			yield(resumeMessage, nil)
			return
		}
		if err := a.prepareInvocation(ctx, invocation); err != nil {
			yield(nil, err)
			return
		}
		ctx = NewAgentContext(ctx, &agentContext{
			name:        a.name,
			description: a.description,
		})
		handler := Handler(HandleFunc(func(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
			req := &ModelRequest{
				Tools:        invocation.Tools,
				Instruction:  invocation.Instruction,
				InputSchema:  a.inputSchema,
				OutputSchema: a.outputSchema,
			}
			if len(invocation.History) > 0 {
				req.Messages = append(req.Messages, invocation.History...)
			}
			if invocation.Message != nil {
				req.Messages = append(req.Messages, invocation.Message)
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

func (a *agent) findResumeMessage(_ context.Context, invocation *Invocation) (*Message, bool) {
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
func (a *agent) storeSession(ctx context.Context, invocation *Invocation, message *Message) error {
	if invocation.Session == nil {
		return nil
	}
	message.Author = a.name
	message.InvocationID = invocation.ID
	switch message.Role {
	case RoleUser:
		return invocation.Session.Append(ctx, []*Message{message})
	case RoleTool:
		if message.Status != StatusCompleted {
			return nil
		}
		return invocation.Session.Append(ctx, []*Message{message})
	case RoleAssistant:
		if message.Status != StatusCompleted {
			return nil
		}
		if a.outputKey != "" {
			invocation.Session.PutState(a.outputKey, message.Text())
		}
		return invocation.Session.Append(ctx, []*Message{message})
	}
	return nil
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
	var (
		m sync.Mutex
	)
	actions := maps.Clone(message.Actions)
	if actions == nil {
		actions = make(map[string]any)
	}
	eg, ctx := errgroup.WithContext(ctx)
	for i, part := range message.Parts {
		switch v := any(part).(type) {
		case ToolPart:
			eg.Go(func() error {
				actions := maps.Clone(actions)
				toolCtx := NewToolContext(ctx, &toolContext{
					id:      v.ID,
					name:    v.Name,
					actions: actions,
				})
				part, err := a.handleTools(toolCtx, v)
				if err != nil {
					return err
				}
				m.Lock()
				message.Parts[i] = part
				message.Actions = MergeActions(message.Actions, actions)
				m.Unlock()
				return nil
			})
		}
	}
	return message, eg.Wait()
}

// handle constructs the default handlers for Run and Stream using the provider.
func (a *agent) handle(ctx context.Context, invocation *Invocation, req *ModelRequest) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		var (
			err           error
			finalResponse *ModelResponse
		)
		if err := a.storeSession(ctx, invocation, invocation.Message); err != nil {
			yield(nil, err)
			return
		}
		for i := 0; i < a.maxIterations; i++ {
			if !invocation.Streamable {
				finalResponse, err = a.model.Generate(ctx, req)
				if err != nil {
					yield(nil, err)
					return
				}
				if err := a.storeSession(ctx, invocation, finalResponse.Message); err != nil {
					yield(nil, err)
					return
				}
				if finalResponse.Message.Role == RoleAssistant {
					if !yield(finalResponse.Message, nil) {
						return
					}
				}
			} else {
				streaming := a.model.NewStreaming(ctx, req)
				for finalResponse, err = range streaming {
					if err != nil {
						yield(nil, err)
						return
					}
					if err := a.storeSession(ctx, invocation, finalResponse.Message); err != nil {
						yield(nil, err)
						return
					}
					if finalResponse.Message.Role == RoleTool && finalResponse.Message.Status == StatusCompleted {
						// Skip yielding tool messages during streaming.
						// Tool messages with StatusCompleted indicate that a tool call has been made,
						// but the result of the tool execution is not yet available. These messages
						// will be processed and yielded after the tool execution is complete in the
						// next step of the agent loop. This ensures that only completed tool results
						// are sent to the client, maintaining correct message order and semantics.
						continue
					}
					if !yield(finalResponse.Message, nil) {
						return // early termination
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
				if !yield(toolMessage, nil) {
					return
				}
				// Append the tool response to the message history for the next iteration
				req.Messages = append(req.Messages, toolMessage)
				continue // continue to the next iteration
			}
			return
		}
		// Exceeded maximum iterations
		yield(nil, ErrMaxIterationsExceeded)
	}
}
