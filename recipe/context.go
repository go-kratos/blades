package recipe

import (
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/context/window"
)

// buildContextCompressor constructs a blades.ContextCompressor from a ContextSpec.
// fallbackModelName is used as the summarizer model when ContextSpec.Model is empty.
func buildContextCompressor(spec *ContextSpec, reg ModelResolver, fallbackModelName string) (blades.ContextCompressor, error) {
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
		return summary.NewContextCompressor(opts...), nil

	case ContextStrategyWindow:
		opts := []window.Option{}
		if spec.MaxTokens > 0 {
			opts = append(opts, window.WithMaxTokens(spec.MaxTokens))
		}
		if spec.MaxMessages > 0 {
			opts = append(opts, window.WithMaxMessages(spec.MaxMessages))
		}
		return window.NewContextCompressor(opts...), nil

	default:
		return nil, fmt.Errorf("recipe: unknown context strategy %q", spec.Strategy)
	}
}

// BuildSessionOption returns a blades.SessionOption that installs the ContextCompressor
// described by spec.Context onto a Session at creation time. Callers should use this
// to create their session before running the agent:
//
//	sessOpt, err := recipe.BuildSessionOption(spec, opts...)
//	session := blades.NewSession(sessOpt)
//	runner.Run(ctx, msg, blades.WithSession(session))
//
// Returns nil when spec has no Context field; nil options are safe to pass to blades.NewSession.
func BuildSessionOption(spec *AgentSpec, opts ...BuildOption) (blades.SessionOption, error) {
	if spec == nil || spec.Context == nil {
		return nil, nil
	}
	o := &buildOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.modelRegistry == nil && spec.Context.Strategy == ContextStrategySummarize {
		return nil, fmt.Errorf("recipe: model registry is required for summarize context strategy")
	}
	c, err := buildContextCompressor(spec.Context, o.modelRegistry, spec.Model)
	if err != nil {
		return nil, err
	}
	return blades.WithContextCompressor(c), nil
}
