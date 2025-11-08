package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
)

// Guardrails is a middleware that adds guardrails to the prompt.
type Guardrails struct {
	next blades.Runnable
}

// NewGuardrails creates a new Guardrails middleware.
func NewGuardrails(next blades.Runnable) blades.Runnable {
	return &Guardrails{next}
}

// Run processes the prompt and adds guardrails before passing it to the next runnable.
func (m *Guardrails) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt")
	return m.next.Run(ctx, prompt, opts...)
}

// RunStream processes the prompt in a streaming manner and adds guardrails before passing it to the next runnable.
func (m *Guardrails) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt (streaming)")
	return stream.Observe(m.next.RunStream(ctx, prompt, opts...), func(msg *blades.Message) error {
		log.Println("Streaming with guardrails applied:", msg.Text())
		return nil
	})
}
