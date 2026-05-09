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

// WithPrompt sets the prompt builder.
func WithPrompt(b prompt.Builder) AgentOption {
	return func(a *llmAgent) {
		a.promptBuilder = b
	}
}
