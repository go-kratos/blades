package recipe

import (
	"context"
	"fmt"
	"iter"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/context/window"
)

// buildCompressor constructs a blades.Compressor from a ContextSpec.
// fallbackModelName is used as the summarizer model when ContextSpec.Model is empty.
func buildCompressor(spec *ContextSpec, reg ModelResolver, fallbackModelName string) (blades.Compressor, error) {
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
		return summary.NewCompressor(opts...), nil

	case ContextStrategyWindow:
		opts := []window.Option{}
		if spec.MaxTokens > 0 {
			opts = append(opts, window.WithMaxTokens(spec.MaxTokens))
		}
		if spec.MaxMessages > 0 {
			opts = append(opts, window.WithMaxMessages(spec.MaxMessages))
		}
		return window.NewCompressor(opts...), nil

	default:
		return nil, fmt.Errorf("recipe: unknown context strategy %q", spec.Strategy)
	}
}

// compressorAwareAgent wraps a blades.Agent and overrides the session's
// Compressor for the duration of the run, enabling per-agent context strategies
// independently of any session-level Compressor.
type compressorAwareAgent struct {
	blades.Agent
	compressor blades.Compressor
}

func (a *compressorAwareAgent) Run(ctx context.Context, inv *blades.Invocation) iter.Seq2[*blades.Message, error] {
	if session, ok := blades.FromSessionContext(ctx); ok {
		wrapped := blades.NewSessionWithCompressor(session, a.compressor)
		ctx = blades.NewSessionContext(ctx, wrapped)
	}
	return a.Agent.Run(ctx, inv)
}

// wrapWithCompressor wraps agent with a compressorAwareAgent when spec is non-nil.
// Returns the original agent unchanged when spec is nil.
// fallbackModelName is used as the summarizer model when ContextSpec.Model is empty.
func wrapWithCompressor(agent blades.Agent, spec *ContextSpec, fallbackModelName string, reg ModelResolver) (blades.Agent, error) {
	if spec == nil {
		return agent, nil
	}
	c, err := buildCompressor(spec, reg, fallbackModelName)
	if err != nil {
		return nil, err
	}
	return &compressorAwareAgent{Agent: agent, compressor: c}, nil
}
