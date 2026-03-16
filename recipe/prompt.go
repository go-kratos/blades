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

func withPromptInjection(spec *RecipeSpec, params map[string]any, base blades.Agent) (blades.Agent, error) {
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
