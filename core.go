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

// Generation represents a single generation of a response from the model.
type Generation struct {
	Messages []*Message `json:"message"`
}

// Text extracts the text content from the first text part of the generation.
func (g *Generation) Text() string {
	for _, msg := range g.Messages {
		for _, part := range msg.Parts {
			if text, ok := part.(TextPart); ok {
				return text.Text
			}
		}
	}
	return ""
}

// File returns the first file part of the message, or nil if none exists.
func (g *Generation) File() *FilePart {
	for _, msg := range g.Messages {
		for _, part := range msg.Parts {
			if file, ok := part.(FilePart); ok {
				return &file
			}
		}
	}
	return nil
}

// Data returns the first data part of the message, or nil if none exists.
func (g *Generation) Data() *DataPart {
	for _, msg := range g.Messages {
		for _, part := range msg.Parts {
			if data, ok := part.(DataPart); ok {
				return &data
			}
		}
	}
	return nil
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
