package recipe

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
)

type promptInjectedAgent struct {
	base   blades.Agent
	prompt string
}

type userPromptInjectedAgent struct {
	base   blades.Agent
	prompt string
}

func (a *promptInjectedAgent) Name() string {
	return a.base.Name()
}

func (a *promptInjectedAgent) Description() string {
	return a.base.Description()
}

func (a *promptInjectedAgent) Run(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
	if inv == nil || a.prompt == "" {
		return a.base.Run(ctx, inv)
	}
	next := inv.Clone()
	promptMessage := blades.UserMessage(a.prompt)
	if next.Message == nil {
		next.Message = promptMessage
	} else {
		next.Instruction = blades.MergeParts(blades.SystemMessage(a.prompt), next.Instruction)
	}
	return a.base.Run(ctx, next)
}

func (a *userPromptInjectedAgent) Name() string {
	return a.base.Name()
}

func (a *userPromptInjectedAgent) Description() string {
	return a.base.Description()
}

func (a *userPromptInjectedAgent) Run(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
	if a.prompt == "" {
		return a.base.Run(ctx, inv)
	}
	if inv == nil {
		inv = &blades.Invocation{}
	}
	next := inv.Clone()
	prompt, err := a.renderPrompt(next)
	if err != nil {
		return func(yield func(*blades.Message, error) bool) {
			yield(nil, err)
		}
	}
	promptMessage := blades.UserMessage(prompt)
	if next.Message == nil {
		next.Message = promptMessage
	} else {
		next.EphemeralMessages = append(next.EphemeralMessages, promptMessage)
	}
	return a.base.Run(ctx, next)
}

func (a *userPromptInjectedAgent) renderPrompt(inv *blades.Invocation) (string, error) {
	if inv == nil || inv.Session == nil || !hasTemplateActions(a.prompt) {
		return a.prompt, nil
	}
	return renderTemplate(a.prompt, inv.Session.State())
}

func withPromptInjection(spec *AgentSpec, params map[string]any, base blades.Agent) (blades.Agent, error) {
	return withPromptTemplate(base, fmt.Sprintf("recipe %q", spec.Name), spec.Prompt, params)
}

func withPromptTemplate(base blades.Agent, scope string, promptTemplate string, params map[string]any) (blades.Agent, error) {
	if promptTemplate == "" {
		return base, nil
	}
	prompt, err := renderTemplate(promptTemplate, params)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to render prompt: %w", scope, err)
	}
	return &promptInjectedAgent{
		base:   base,
		prompt: prompt,
	}, nil
}

func withUserPromptTemplate(base blades.Agent, scope string, promptTemplate string, params map[string]any) (blades.Agent, error) {
	if promptTemplate == "" {
		return base, nil
	}
	prompt := promptTemplate
	if hasTemplateActions(promptTemplate) {
		var err error
		prompt, err = renderTemplatePreservingUnknown(promptTemplate, params)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to render prompt: %w", scope, err)
		}
	} else {
		var err error
		prompt, err = renderTemplate(promptTemplate, params)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to render prompt: %w", scope, err)
		}
	}
	return &userPromptInjectedAgent{
		base:   base,
		prompt: prompt,
	}, nil
}
