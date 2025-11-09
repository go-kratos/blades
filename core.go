package blades

import (
	"context"
	"iter"
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
	for _, m := range p.Messages {
		buf.WriteString(m.Text())
		buf.WriteByte('\n')
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Sequence returns an iterator over the messages in the prompt.
type Sequence[T any] = iter.Seq2[T, error]

// Agent represents an autonomous agent that can perform tasks based on invocations.
type Agent interface {
	Name() string
	Description() string
	Run(context.Context, *Invocation) Sequence[*Message]
}
