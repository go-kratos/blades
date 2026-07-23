package model

import "github.com/go-kratos/blades/content"

// Role indicates the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// MessageMetadata contains provider attribution for a message.
type MessageMetadata struct {
	// Provider identifies the service that served the model.
	Provider string
	// API identifies the provider protocol used for the model call.
	API string
	// Model identifies the model that generated the message.
	Model string
}

// Message is the model-layer representation of a conversation turn.
type Message struct {
	Role     Role
	Parts    []content.Part
	Metadata MessageMetadata
}
