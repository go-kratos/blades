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

func (m *Logging) onError(start time.Time, prompt *blades.Prompt, err error) {
	log.Printf("logging: prompt(%s) failed after %s: %v", prompt.String(), time.Since(start), err)
}

func (m *Logging) onSuccess(start time.Time, prompt *blades.Prompt, message *blades.Message) {
	log.Printf("logging: prompt(%s) succeeded after %s: %s", prompt.String(), time.Since(start), message.String())
}

func (m *Logging) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	start := time.Now()
	message, err := m.next.Run(ctx, prompt, opts...)
	if err != nil {
		m.onError(start, prompt, err)
	} else {
		m.onSuccess(start, prompt, message)
	}
	return message, err
}

func (m *Logging) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	return func(yield func(*blades.Message, error) bool) {
		start := time.Now()
		streaming := m.next.RunStream(ctx, prompt, opts...)
		for msg, err := range streaming {
			if err != nil {
				m.onError(start, prompt, err)
			} else {
				m.onSuccess(start, prompt, msg)
			}
			if !yield(msg, err) {
				break
			}
		}
	}
}
