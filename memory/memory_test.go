package memory

import (
	"testing"

	"github.com/go-kratos/blades"
)

func TestMemory(t *testing.T) {
	message := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleUser,
		Parts: []blades.Part{blades.TextPart{Text: "Hello, world!"}},
	}

	metadata := map[string]any{
		"source":    "test",
		"timestamp": "2023-01-01",
	}

	memory := &Memory{
		Content:  message,
		Metadata: metadata,
	}

	if memory.Content != message {
		t.Errorf("Memory.Content = %v, want %v", memory.Content, message)
	}
	if memory.Metadata["source"] != "test" {
		t.Errorf("Memory.Metadata[source] = %v, want test", memory.Metadata["source"])
	}
	if memory.Metadata["timestamp"] != "2023-01-01" {
		t.Errorf("Memory.Metadata[timestamp] = %v, want 2023-01-01", memory.Metadata["timestamp"])
	}
}

func TestMemoryWithoutMetadata(t *testing.T) {
	message := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleUser,
		Parts: []blades.Part{blades.TextPart{Text: "Hello, world!"}},
	}

	memory := &Memory{
		Content: message,
	}

	if memory.Content != message {
		t.Errorf("Memory.Content = %v, want %v", memory.Content, message)
	}
	if memory.Metadata != nil {
		t.Errorf("Memory.Metadata = %v, want nil", memory.Metadata)
	}
}
