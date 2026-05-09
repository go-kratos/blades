package model

import "github.com/go-kratos/blades/content"

// Role indicates the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is the model-layer representation of a conversation turn.
type Message struct {
	Role  Role
	Parts []content.Part
}
