package main

import (
	"context"
	"log"
	"time"

	"github.com/go-kratos/blades"
)

type Logging struct {
	next blades.Handler
}

// NewLogging creates a new Logging middleware.
func NewLogging(next blades.Handler) blades.Handler {
	return &Logging{next}
}

func (m *Logging) onError(start time.Time, agent blades.AgentContext, invocation *blades.Invocation, err error) {
	log.Printf("logging: model(%s) prompt(%s) failed after %s: %v", agent.Model(), invocation.Message.String(), time.Since(start), err)
}

func (m *Logging) onSuccess(start time.Time, agent blades.AgentContext, invocation *blades.Invocation, output *blades.Message) {
	log.Printf("logging: model(%s) prompt(%s) succeeded after %s: %s", agent.Model(), invocation.Message.String(), time.Since(start), output.String())
}

func (m *Logging) Handle(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		start := time.Now()
		agent, ok := blades.FromAgentContext(ctx)
		if !ok {
			yield(nil, blades.ErrNoAgentContext)
			return
		}
		streaming := m.next.Handle(ctx, invocation)
		for msg, err := range streaming {
			if err != nil {
				m.onError(start, agent, invocation, err)
			} else {
				m.onSuccess(start, agent, invocation, msg)
			}
			if !yield(msg, err) {
				break
			}
		}
	}
}
