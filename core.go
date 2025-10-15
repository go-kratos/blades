package blades

import (
	"context"
	"strings"
)

// Prompt represents a sequence of messages exchanged between a user and an assistant.
type Prompt struct {
	Messages []*Message `json:"messages"`
}

// NewPrompt creates a new Prompt with the given messages.
func NewPrompt(messages ...*Message) *Prompt {
	return &Prompt{
		Messages: messages,
	}
}

// Latest returns the most recent message in the prompt, or nil if there are no messages.
func (p *Prompt) Latest() *Message {
	if len(p.Messages) == 0 {
		return nil
	}
	return p.Messages[len(p.Messages)-1]
}

// String returns the string representation of the prompt by concatenating all message strings.
func (p *Prompt) String() string {
	var buf strings.Builder
	for _, msg := range p.Messages {
		buf.WriteString(msg.String())
	}
	return buf.String()
}

// Streamable yields a sequence of assistant responses until completion.
type Streamable[T any] interface {
	Next() bool
	Current() (T, error)
	Close() error
}

// Runnable represents an entity that can process prompts and generate responses.
type Runnable[Input, Output, Option any] interface {
	Name() string
	Run(context.Context, Input, ...Option) (Output, error)
	RunStream(context.Context, Input, ...Option) (Streamable[Output], error)
}
