package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
)

// Guardrails is a middleware that adds guardrails to the prompt.
type Guardrails struct {
	next blades.Handler
}

// NewGuardrails creates a new Guardrails middleware.
func NewGuardrails(next blades.Handler) blades.Handler {
	return &Guardrails{next}
}

func (m *Guardrails) Handle(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt (streaming)")
	return stream.Observe(m.next.Handle(ctx, invocation), func(msg *blades.Message, err error) error {
		if err != nil {
			log.Println("Error during streaming:", err)
			return err
		}
		log.Println("Streaming with guardrails applied:", msg.Text())
		return nil
	})
}
