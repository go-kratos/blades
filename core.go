package blades

import (
	"context"
	"strings"

	"github.com/go-kratos/blades/stream"
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

// Runnable represents an entity that can process prompts and generate responses.
type Runnable interface {
	Run(context.Context, *Prompt, ...ModelOption) (*Message, error)
	RunStream(context.Context, *Prompt, ...ModelOption) stream.Streamable[*Message]
}
