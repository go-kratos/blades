package blades

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-kratos/blades/skills"
	"github.com/go-kratos/blades/tools"
	"github.com/go-kratos/kit/container/maps"
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

// WithInstruction sets the instruction for the Agent.
func WithInstruction(instruction string) AgentOption {
	return func(a *agent) {
		a.instruction = instruction
	}
}

// WithInstructionProvider sets a dynamic instruction provider for the Agent.
func WithInstructionProvider(p InstructionProvider) AgentOption {
	return func(a *agent) {
		a.instructionProvider = p
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

// WithSkills sets skills for the Agent.
func WithSkills(skillList ...skills.Skill) AgentOption {
	return func(a *agent) {
		a.skills = skillList
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
	name                string
	description         string
	instruction         string
	instructionProvider InstructionProvider
	outputKey           string
	maxIterations       int
	model               ModelProvider
	inputSchema         *jsonschema.Schema
	outputSchema        *jsonschema.Schema
	middlewares         []Middleware
	tools               []tools.Tool
	skills              []skills.Skill
	skillToolset        *skills.Toolset
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
	if len(a.skills) > 0 {
		toolset, err := skills.NewToolset(a.skills)
		if err != nil {
			return nil, err
		}
		a.skillToolset = toolset
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
	if invocation.committed == nil {
		invocation.committed = new(atomic.Bool)
	}
	resolvedTools, err := a.resolveTools(ctx)
	if err != nil {
		return err
	}
	invocation.Model = a.model.Name()
	finalTools := resolvedTools
	if a.skillToolset != nil {
		finalTools = a.skillToolset.ComposeTools(resolvedTools)
		invocation.Instruction = MergeParts(SystemMessage(a.skillToolset.Instruction()), invocation.Instruction)
	}
	invocation.Tools = append(invocation.Tools, finalTools...)
	// order of precedence: static instruction > instruction provider > skills instruction > invocation instruction
	if a.instructionProvider != nil {
		instruction, err := a.instructionProvider(ctx)
		if err != nil {
			return err
		}
		invocation.Instruction = MergeParts(SystemMessage(instruction), invocation.Instruction)
	}
	if a.instruction != "" {
		if invocation.Session != nil {
			var buf strings.Builder
			t, err := template.New("instruction").Parse(a.instruction)
			if err != nil {
				return err
			}
			if err := t.Execute(&buf, invocation.Session.State()); err != nil {
				return err
			}
			invocation.Instruction = MergeParts(SystemMessage(buf.String()), invocation.Instruction)
		} else {
			invocation.Instruction = MergeParts(SystemMessage(a.instruction), invocation.Instruction)
		}
	}
	return nil
}

// Run runs the agent with the given prompt and options, returning a streamable response.
func (a *agent) Run(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		// Ensure a session exists in context so every phase of this Run (including
		// the iteration loop in handle) uses the same session instance.
		if _, ok := SessionFromContext(ctx); !ok {
			ctx = NewSessionContext(ctx, NewSession())
		}
		session := EnsureSession(ctx)
		// prepareInvocation initializes committed if nil, so the CAS below is safe.
		if err := a.prepareInvocation(ctx, invocation); err != nil {
			yield(nil, err)
			return
		}
		// Append the initial user message exactly once per run lifecycle.
		// All clones share the same *atomic.Bool; the first CompareAndSwap wins.
		if !invocation.Resume && invocation.Message != nil && invocation.committed.CompareAndSwap(false, true) {
			msg := invocation.Message
			if msg.Author == "" {
				msg.Author = "user"
			}
			if msg.InvocationID == "" {
				msg.InvocationID = invocation.ID
			}
			if err := session.Append(ctx, msg); err != nil {
				yield(nil, err)
				return
			}
		}
		ctx = NewAgentContext(ctx, a)
		handler := Handler(HandleFunc(func(ctx context.Context, invocation *Invocation) Generator[*Message, error] {
			req := &ModelRequest{
				Tools:        invocation.Tools,
				Instruction:  invocation.Instruction,
				InputSchema:  a.inputSchema,
				OutputSchema: a.outputSchema,
			}
			return a.handle(ctx, session, invocation, req)
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

func (a *agent) saveOutputState(ctx context.Context, invocation *Invocation, message *Message) {
	if a.outputKey != "" &&
		invocation.Session != nil &&
		message.Role == RoleAssistant &&
		message.Status == StatusCompleted {
		invocation.Session.SetState(a.outputKey, message.Text())
	}
}

func (a *agent) handleTools(ctx context.Context, invocation *Invocation, part ToolPart) (ToolPart, error) {
	// Search through all available tools (static + resolved)
	for _, tool := range invocation.Tools {
		if tool.Name() == part.Name {
			response, err := tool.Handle(ctx, part.Request)
			if err != nil {
				return part, err
			}
			part.Response = response
			return part, nil
		}
	}
	return part, fmt.Errorf("agent: tool %s not found", part.Name)
}

// executeTools executes the tools specified in the tool parts.
func (a *agent) executeTools(ctx context.Context, invocation *Invocation, message *Message) (*Message, error) {
	var (
		m sync.Mutex
	)
	actions := maps.New(message.Actions)
	eg, ctx := errgroup.WithContext(ctx)
	for i, part := range message.Parts {
		switch v := any(part).(type) {
		case ToolPart:
			eg.Go(func() error {
				if v.Completed {
					return nil
				}
				toolCtx := tools.NewContext(ctx, &toolContext{
					id:      v.ID,
					name:    v.Name,
					actions: actions,
				})
				part, err := a.handleTools(toolCtx, invocation, v)
				if err != nil {
					return err
				}
				part.Completed = true
				m.Lock()
				message.Parts[i] = part
				message.Actions = MergeActions(message.Actions, actions.ToMap())
				m.Unlock()
				return nil
			})
		}
	}
	return message, eg.Wait()
}

func messageFromResponse(response *ModelResponse) (*Message, error) {
	if response == nil || response.Message == nil {
		return nil, ErrNoFinalResponse
	}
	return response.Message, nil
}

// handle constructs the default handlers for Run and Stream using the provider.
func (a *agent) handle(ctx context.Context, session Session, invocation *Invocation, req *ModelRequest) Generator[*Message, error] {
	return func(yield func(*Message, error) bool) {
		for i := 0; i < a.maxIterations; i++ {
			// Rebuild req.Messages each iteration so context compression
			// (applied inside session.History) can re-run on the growing list.
			prepared, err := session.History(ctx)
			if err != nil {
				yield(nil, err)
				return
			}
			req.Messages = prepared
			var finalMessage *Message
			if !invocation.Stream {
				finalResponse, err := a.model.Generate(ctx, req)
				if err != nil {
					yield(nil, err)
					return
				}
				finalMessage, err = messageFromResponse(finalResponse)
				if err != nil {
					yield(nil, err)
					return
				}
				if finalMessage.Author == "" {
					finalMessage.Author = a.name
				}
				finalMessage.InvocationID = invocation.ID
				// Skip saving tool intermediate states
				if finalMessage.Role == RoleAssistant {
					a.saveOutputState(ctx, invocation, finalMessage)
					if !yield(finalMessage, nil) {
						return
					}
				}
			} else {
				streaming := a.model.NewStreaming(ctx, req)
				for response, err := range streaming {
					if err != nil {
						yield(nil, err)
						return
					}
					finalMessage, err = messageFromResponse(response)
					if err != nil {
						yield(nil, err)
						return
					}
					if finalMessage.Author == "" {
						finalMessage.Author = a.name
					}
					finalMessage.InvocationID = invocation.ID
					// Skip saving tool intermediate states
					if finalMessage.Role == RoleTool && finalMessage.Status == StatusCompleted {
						continue
					}
					a.saveOutputState(ctx, invocation, finalMessage)
					if !yield(finalMessage, nil) {
						return // early termination
					}
				}
			}
			if finalMessage == nil {
				yield(nil, ErrNoFinalResponse)
				return
			}
			if invocation.Stream && finalMessage.Status != StatusCompleted {
				yield(nil, ErrNoFinalResponse)
				return
			}
			if finalMessage.Role == RoleTool {
				toolMessage, err := a.executeTools(ctx, invocation, finalMessage)
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(toolMessage, nil) {
					return
				}
				// Persist the tool response; the next iteration's session.History()
				// call will include it automatically.
				if err := session.Append(ctx, toolMessage); err != nil {
					yield(nil, err)
					return
				}
				continue // continue to the next iteration
			}
			// Persist the final assistant message so future invocations can
			// access the full conversation history via session.History().
			if err := session.Append(ctx, finalMessage); err != nil {
				yield(nil, err)
				return
			}
			return
		}
		// Exceeded maximum iterations
		yield(nil, ErrMaxIterationsExceeded)
	}
}
