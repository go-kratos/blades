package blades

import (
	"context"
	"strings"
)

// Prompt represents a sequence of messages exchanged between a user and an assistant.
type Prompt struct {
	ConversationID string     `json:"conversation_id,omitempty"`
	Messages       []*Message `json:"messages"`
}

// NewPrompt creates a new Prompt with the given messages.
func NewPrompt(messages ...*Message) *Prompt {
	return &Prompt{
		Messages: messages,
	}
}

// NewConversation creates a new Prompt bound to a conversation ID.
// When used with memory, the conversation history keyed by this ID is loaded.
func NewConversation(conversationID string, messages ...*Message) *Prompt {
	return &Prompt{
		ConversationID: conversationID,
		Messages:       messages,
	}
}

func (p *Prompt) String() string {
	var buf strings.Builder
	for _, msg := range p.Messages {
		buf.WriteString(msg.String())
	}
	return buf.String()
}

// Generation represents a single generation of a response from the model.
type Generation struct {
	Messages []*Message `json:"message"`
}

func (g *Generation) AsText() string {
	for _, msg := range g.Messages {
		for _, part := range msg.Parts {
			if text, ok := part.(TextPart); ok {
				return text.Text
			}
		}
	}
	return ""
}

// Streamer yields a sequence of assistant responses until completion.
type Streamer[T any] interface {
	Next() bool
	Current() (T, error)
	Close() error
}

// Runner represents an entity that can process prompts and generate responses.
type Runner interface {
	Run(context.Context, *Prompt, ...ModelOption) (*Generation, error)
	RunStream(context.Context, *Prompt, ...ModelOption) (Streamer[*Generation], error)
}
