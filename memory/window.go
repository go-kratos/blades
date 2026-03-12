package memory

import (
	"context"

	"github.com/go-kratos/blades"
)

// WindowConfig holds configuration for the window-based context manager.
type WindowConfig struct {
	// MaxMessages is the maximum number of messages to keep. 0 means no limit.
	MaxMessages int
	// MaxTokens is the maximum total token budget. 0 means no limit.
	MaxTokens int64
	// Counter is used to estimate token usage. Defaults to charBasedCounter.
	Counter blades.TokenCounter
}

// windowContextManager implements a sliding-window context strategy.
// It retains the most recent messages that fit within MaxMessages or MaxTokens.
type windowContextManager struct {
	cfg WindowConfig
}

// NewWindowContextManager returns a ContextManager that keeps the most recent
// messages within the configured token or message count limits. Messages are
// dropped from the front (oldest first) when limits are exceeded.
func NewWindowContextManager(cfg WindowConfig) blades.ContextManager {
	return &windowContextManager{cfg: cfg}
}

// Prepare retains the most recent messages that fit the configured limits.
func (w *windowContextManager) Prepare(_ context.Context, messages []*blades.Message) ([]*blades.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	counter := w.counter()

	// Apply message count limit first.
	result := messages
	if w.cfg.MaxMessages > 0 && len(result) > w.cfg.MaxMessages {
		result = result[len(result)-w.cfg.MaxMessages:]
	}

	// Apply token limit by dropping from the front until we fit.
	if w.cfg.MaxTokens > 0 {
		for len(result) > 1 && counter.Count(result...) > w.cfg.MaxTokens {
			result = result[1:]
		}
	}

	return result, nil
}

func (w *windowContextManager) counter() blades.TokenCounter {
	if w.cfg.Counter != nil {
		return w.cfg.Counter
	}
	return &charBasedCounter{}
}
