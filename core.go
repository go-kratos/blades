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

// String returns the string representation of the prompt by concatenating all message strings.
func (p *Prompt) String() string {
	var buf strings.Builder
	for _, msg := range p.Messages {
		buf.WriteString(msg.String())
	}
	return buf.String()
}

// Streamer yields a sequence of assistant responses until completion.
type Streamer[T any] interface {
	Next() bool
	Current() (T, error)
	Close() error
}

// Runner represents an entity that can process prompts and generate responses.
type Runner[Input, Output, Option any] interface {
	Name() string
	Run(context.Context, Input, ...Option) (Output, error)
	RunStream(context.Context, Input, ...Option) (Streamer[Output], error)
}

// ModelRunner is a Runner specialized for processing Prompts and generating Generations with ModelOptions.
type ModelRunner Runner[*Prompt, *Message, ModelOption]
