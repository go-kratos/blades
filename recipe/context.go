package recipe

import (
	"context"
	"fmt"
	"iter"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/context/window"
)

// buildContextManager constructs a blades.ContextManager from a ContextSpec.
// fallbackModelName is used as the summarizer model when ContextSpec.Model is empty.
func buildContextManager(spec *ContextSpec, reg ModelResolver, fallbackModelName string) (blades.ContextManager, error) {
	if spec == nil {
		return nil, nil
	}
	switch spec.Strategy {
	case ContextStrategySummarize:
		opts := []summary.Option{}
		if spec.MaxTokens > 0 {
			opts = append(opts, summary.WithMaxTokens(spec.MaxTokens))
		}
		if spec.KeepRecent > 0 {
			opts = append(opts, summary.WithKeepRecent(spec.KeepRecent))
		}
		if spec.BatchSize > 0 {
			opts = append(opts, summary.WithBatchSize(spec.BatchSize))
		}
		modelName := spec.Model
		if modelName == "" {
			modelName = fallbackModelName
		}
		if modelName != "" {
			model, err := reg.Resolve(modelName)
			if err != nil {
				return nil, fmt.Errorf("recipe: context model: %w", err)
			}
			opts = append(opts, summary.WithSummarizer(model))
		}
		return summary.NewContextManager(opts...), nil

	case ContextStrategyWindow:
		opts := []window.Option{}
		if spec.MaxTokens > 0 {
			opts = append(opts, window.WithMaxTokens(spec.MaxTokens))
		}
		if spec.MaxMessages > 0 {
			opts = append(opts, window.WithMaxMessages(spec.MaxMessages))
		}
		return window.NewContextManager(opts...), nil

	default:
		return nil, fmt.Errorf("recipe: unknown context strategy %q", spec.Strategy)
	}
}

// contextAwareAgent wraps a blades.Agent and injects a ContextManager into
// the execution context before each run, enabling per-agent context strategies
// independently of any Runner-level ContextManager.
type contextAwareAgent struct {
	blades.Agent
	cm blades.ContextManager
}

func (a *contextAwareAgent) Run(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
	ctx = blades.NewContextManagerContext(ctx, a.cm)
	return a.Agent.Run(ctx, inv)
}

// wrapWithContextManager wraps agent with a contextAwareAgent when spec is non-nil.
// Returns the original agent unchanged when spec is nil.
// fallbackModelName is used as the summarizer model when ContextSpec.Model is empty.
func wrapWithContextManager(agent blades.Agent, spec *ContextSpec, fallbackModelName string, reg ModelResolver) (blades.Agent, error) {
	if spec == nil {
		return agent, nil
	}
	cm, err := buildContextManager(spec, reg, fallbackModelName)
	if err != nil {
		return nil, err
	}
	return &contextAwareAgent{Agent: agent, cm: cm}, nil
}
