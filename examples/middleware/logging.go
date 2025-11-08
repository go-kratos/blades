package main

import (
	"context"
	"log"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
)

type Logging struct {
	next blades.Runnable
}

// NewLogging creates a new Logging middleware.
func NewLogging(next blades.Runnable) blades.Runnable {
	return &Logging{next}
}

func (m *Logging) onError(agent blades.AgentContext, start time.Time, prompt *blades.Prompt, err error) {
	log.Printf("logging: model(%s) prompt(%s) failed after %s: %v", agent.Model(), prompt.String(), time.Since(start), err)
}

func (m *Logging) onSuccess(agent blades.AgentContext, start time.Time, prompt *blades.Prompt, message *blades.Message) {
	log.Printf("logging: model(%s) prompt(%s) succeeded after %s: %s", agent.Model(), prompt.String(), time.Since(start), message.String())
}

func (m *Logging) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	start := time.Now()
	agent, ok := blades.FromAgentContext(ctx)
	if !ok {
		return nil, blades.ErrNoAgentContext
	}
	message, err := m.next.Run(ctx, prompt, opts...)
	if err != nil {
		m.onError(agent, start, prompt, err)
	} else {
		m.onSuccess(agent, start, prompt, message)
	}
	return message, err
}

func (m *Logging) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	return func(yield func(*blades.Message, error) bool) {
		start := time.Now()
		agent, ok := blades.FromAgentContext(ctx)
		if !ok {
			yield(nil, blades.ErrNoAgentContext)
			return
		}
		streaming := m.next.RunStream(ctx, prompt, opts...)
		for msg, err := range streaming {
			if err != nil {
				m.onError(agent, start, prompt, err)
			} else {
				m.onSuccess(agent, start, prompt, msg)
			}
			if !yield(msg, err) {
				break
			}
		}
	}
}
