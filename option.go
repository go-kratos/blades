package blades

import (
	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/skills"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// AgentOption configures an llmAgent.
type AgentOption func(*llmAgent)

// WithModel sets the model provider.
func WithModel(p model.Provider) AgentOption {
	return func(a *llmAgent) {
		a.provider = p
	}
}

// WithDescription sets the agent description.
func WithDescription(desc string) AgentOption {
	return func(a *llmAgent) {
		a.description = desc
	}
}

// WithTools sets the static tool list.
func WithTools(t ...tools.Tool) AgentOption {
	return func(a *llmAgent) {
		a.tools = t
	}
}

// WithToolsResolver sets a dynamic tool resolver.
func WithToolsResolver(r tools.Resolver) AgentOption {
	return func(a *llmAgent) {
		a.resolver = r
	}
}

// WithSkills adds reusable instructions that the model can load on demand.
func WithSkills(skillList ...skills.Skill) AgentOption {
	return func(a *llmAgent) {
		a.skills = append(a.skills, skillList...)
	}
}

// WithPolicy sets the tool invocation policy.
func WithPolicy(p policy.Policy) AgentOption {
	return func(a *llmAgent) {
		a.policy = p
	}
}

// WithHooks sets lifecycle hooks.
func WithHooks(h ...hook.Hook) AgentOption {
	return func(a *llmAgent) {
		a.hooks = h
	}
}

// WithCompact sets the context compactor.
func WithCompact(c compact.Compactor) AgentOption {
	return func(a *llmAgent) {
		a.compactor = c
	}
}

// WithContextWindow sets the model's context window and compaction threshold.
func WithContextWindow(w model.ContextWindow) AgentOption {
	return func(a *llmAgent) {
		a.contextWindow = w
	}
}

// WithTokenCounter sets the request-level token counter used for context
// stats and compaction threshold checks. When nil, the Agent uses the
// default approximate counter.
func WithTokenCounter(counter model.TokenCounter) AgentOption {
	return func(a *llmAgent) {
		if counter == nil {
			a.tokenCounter = model.ApproxTokenCounter{}
			return
		}
		a.tokenCounter = counter
	}
}

// WithThinkingAsText controls a compatibility fallback for models that emit
// assistant messages containing only thinking content. When enabled, the
// Agent exposes that content as text in the AssistantMessageEnd event. The
// model response, streaming deltas, turn result, and session remain unchanged.
func WithThinkingAsText(enabled bool) AgentOption {
	return func(a *llmAgent) {
		a.thinkingAsText = enabled
	}
}

// WithInstruction appends a static instruction to the system prompt.
func WithInstruction(instruction string) AgentOption {
	return func(a *llmAgent) {
		a.promptBuilders = append(a.promptBuilders, prompt.Text(instruction))
	}
}

// WithPrompt appends a prompt builder.
func WithPrompt(b prompt.Builder) AgentOption {
	return func(a *llmAgent) {
		a.promptBuilders = append(a.promptBuilders, b)
	}
}

// WithInputSchema sets the JSON schema describing the agent's expected input.
func WithInputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *llmAgent) {
		a.inputSchema = schema
	}
}

// WithOutputSchema sets the JSON schema describing the agent's output format.
func WithOutputSchema(schema *jsonschema.Schema) AgentOption {
	return func(a *llmAgent) {
		a.outputSchema = schema
	}
}
