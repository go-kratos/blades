package blades

import (
	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/tools"
)

// AgentOption configures an llmAgent.
type AgentOption func(*llmAgent)

// WithModel sets the model provider.
func WithModel(p model.Provider) AgentOption {
	return func(a *llmAgent) {
		a.provider = p
		if a.tokenCounterSet {
			return
		}
		if counter, ok := p.(model.TokenCounter); ok {
			a.tokenCounter = counter
		} else {
			a.tokenCounter = nil
		}
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

// WithContextBudget sets request-view budgets for the default Agent runtime.
func WithContextBudget(b ContextBudget) AgentOption {
	return func(a *llmAgent) {
		a.contextBudget = b
	}
}

// WithTokenCounter sets the request-level token counter used for context stats
// and budget enforcement.
func WithTokenCounter(counter model.TokenCounter) AgentOption {
	return func(a *llmAgent) {
		a.tokenCounter = counter
		a.tokenCounterSet = true
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
