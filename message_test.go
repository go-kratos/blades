package blades

import (
	"testing"
)

func TestRole(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		expected string
	}{
		{"User role", RoleUser, "user"},
		{"System role", RoleSystem, "system"},
		{"Assistant role", RoleAssistant, "assistant"},
		{"Tool role", RoleTool, "tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.role) != tt.expected {
				t.Errorf("Role = %v, want %v", tt.role, tt.expected)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		expected string
	}{
		{"InProgress status", StatusInProgress, "in_progress"},
		{"Incomplete status", StatusIncomplete, "incomplete"},
		{"Completed status", StatusCompleted, "completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Status = %v, want %v", tt.status, tt.expected)
			}
		})
	}
}

func TestTextPart(t *testing.T) {
	text := "Hello, world!"
	part := TextPart{Text: text}

	if part.Text != text {
		t.Errorf("TextPart.Text = %v, want %v", part.Text, text)
	}

	// Test isPart interface
	var _ Part = part
}

func TestFilePart(t *testing.T) {
	file := FilePart{
		Name:     "test.txt",
		URI:      "file:///path/to/test.txt",
		MIMEType: MIMEText,
	}

	if file.Name != "test.txt" {
		t.Errorf("FilePart.Name = %v, want test.txt", file.Name)
	}
	if file.URI != "file:///path/to/test.txt" {
		t.Errorf("FilePart.URI = %v, want file:///path/to/test.txt", file.URI)
	}
	if file.MIMEType != MIMEText {
		t.Errorf("FilePart.MIMEType = %v, want %v", file.MIMEType, MIMEText)
	}

	// Test isPart interface
	var _ Part = file
}

func TestDataPart(t *testing.T) {
	data := []byte("Hello, world!")
	part := DataPart{
		Name:     "test.txt",
		Bytes:    data,
		MIMEType: MIMEText,
	}

	if part.Name != "test.txt" {
		t.Errorf("DataPart.Name = %v, want test.txt", part.Name)
	}
	if string(part.Bytes) != "Hello, world!" {
		t.Errorf("DataPart.Bytes = %v, want Hello, world!", string(part.Bytes))
	}
	if part.MIMEType != MIMEText {
		t.Errorf("DataPart.MIMEType = %v, want %v", part.MIMEType, MIMEText)
	}

	// Test isPart interface
	var _ Part = part
}

func TestToolPart(t *testing.T) {
	part := ToolPart{
		ID:       "tool-123",
		Name:     "calculator",
		Request:  `{"operation": "add", "a": 1, "b": 2}`,
		Response: "3",
	}

	if part.ID != "tool-123" {
		t.Errorf("ToolPart.ID = %v, want tool-123", part.ID)
	}
	if part.Name != "calculator" {
		t.Errorf("ToolPart.Name = %v, want calculator", part.Name)
	}
	if part.Request != `{"operation": "add", "a": 1, "b": 2}` {
		t.Errorf("ToolPart.Request = %v, want %v", part.Request, `{"operation": "add", "a": 1, "b": 2}`)
	}
	if part.Response != "3" {
		t.Errorf("ToolPart.Response = %v, want 3", part.Response)
	}

	// Test isPart interface
	var _ Part = part
}

func TestMessage(t *testing.T) {
	msg := &Message{
		ID:     "msg-123",
		Role:   RoleUser,
		Parts:  []Part{TextPart{Text: "Hello"}},
		Status: StatusCompleted,
		Metadata: map[string]string{
			"source": "test",
		},
	}

	if msg.ID != "msg-123" {
		t.Errorf("Message.ID = %v, want msg-123", msg.ID)
	}
	if msg.Role != RoleUser {
		t.Errorf("Message.Role = %v, want %v", msg.Role, RoleUser)
	}
	if len(msg.Parts) != 1 {
		t.Errorf("Message.Parts length = %v, want 1", len(msg.Parts))
	}
	if msg.Status != StatusCompleted {
		t.Errorf("Message.Status = %v, want %v", msg.Status, StatusCompleted)
	}
	if msg.Metadata["source"] != "test" {
		t.Errorf("Message.Metadata[source] = %v, want test", msg.Metadata["source"])
	}
}

func TestMessageText(t *testing.T) {
	tests := []struct {
		name     string
		parts    []Part
		expected string
	}{
		{
			"Single text part",
			[]Part{TextPart{Text: "Hello"}},
			"Hello",
		},
		{
			"Multiple text parts",
			[]Part{TextPart{Text: "Hello"}, TextPart{Text: "World"}},
			"Hello\nWorld",
		},
		{
			"Mixed parts",
			[]Part{TextPart{Text: "Hello"}, FilePart{Name: "test.txt"}},
			"Hello",
		},
		{
			"No text parts",
			[]Part{FilePart{Name: "test.txt"}},
			"",
		},
		{
			"Empty parts",
			[]Part{},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Parts: tt.parts}
			result := msg.Text()
			if result != tt.expected {
				t.Errorf("Message.Text() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMessageFile(t *testing.T) {
	tests := []struct {
		name     string
		parts    []Part
		expected *FilePart
	}{
		{
			"Single file part",
			[]Part{FilePart{Name: "test.txt", URI: "file://test.txt"}},
			&FilePart{Name: "test.txt", URI: "file://test.txt"},
		},
		{
			"Multiple file parts",
			[]Part{FilePart{Name: "test1.txt"}, FilePart{Name: "test2.txt"}},
			&FilePart{Name: "test1.txt"},
		},
		{
			"No file parts",
			[]Part{TextPart{Text: "Hello"}},
			nil,
		},
		{
			"Empty parts",
			[]Part{},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Parts: tt.parts}
			result := msg.File()
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Message.File() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Errorf("Message.File() = nil, want %v", tt.expected)
				} else if result.Name != tt.expected.Name {
					t.Errorf("Message.File().Name = %v, want %v", result.Name, tt.expected.Name)
				}
			}
		})
	}
}

func TestMessageData(t *testing.T) {
	tests := []struct {
		name     string
		parts    []Part
		expected *DataPart
	}{
		{
			"Single data part",
			[]Part{DataPart{Name: "test.txt", Bytes: []byte("data")}},
			&DataPart{Name: "test.txt", Bytes: []byte("data")},
		},
		{
			"Multiple data parts",
			[]Part{DataPart{Name: "test1.txt"}, DataPart{Name: "test2.txt"}},
			&DataPart{Name: "test1.txt"},
		},
		{
			"No data parts",
			[]Part{TextPart{Text: "Hello"}},
			nil,
		},
		{
			"Empty parts",
			[]Part{},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{Parts: tt.parts}
			result := msg.Data()
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Message.Data() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Errorf("Message.Data() = nil, want %v", tt.expected)
				} else if result.Name != tt.expected.Name {
					t.Errorf("Message.Data().Name = %v, want %v", result.Name, tt.expected.Name)
				}
			}
		})
	}
}

func TestMessageString(t *testing.T) {
	msg := &Message{
		Parts: []Part{
			TextPart{Text: "Hello"},
			FilePart{Name: "test.txt", MIMEType: MIMEText},
			DataPart{Name: "data.txt", Bytes: []byte("data"), MIMEType: MIMEText},
			ToolPart{Name: "calculator", Request: "1+1", Response: "2"},
		},
	}

	result := msg.String()
	expected := "[Text: Hello)][File: test.txt (text/plain)][Data: data.txt (text/plain), 4 bytes][Tool: calculator (Request: 1+1, Response: 2)]"

	if result != expected {
		t.Errorf("Message.String() = %q, want %q", result, expected)
	}
}

func TestUserMessage(t *testing.T) {
	msg := UserMessage("Hello", "World")

	if msg.Role != RoleUser {
		t.Errorf("UserMessage.Role = %v, want %v", msg.Role, RoleUser)
	}
	if len(msg.Parts) != 2 {
		t.Errorf("UserMessage.Parts length = %v, want 2", len(msg.Parts))
	}
	if msg.ID == "" {
		t.Errorf("UserMessage.ID should not be empty")
	}
}

func TestSystemMessage(t *testing.T) {
	msg := SystemMessage("System prompt")

	if msg.Role != RoleSystem {
		t.Errorf("SystemMessage.Role = %v, want %v", msg.Role, RoleSystem)
	}
	if len(msg.Parts) != 1 {
		t.Errorf("SystemMessage.Parts length = %v, want 1", len(msg.Parts))
	}
	if msg.ID == "" {
		t.Errorf("SystemMessage.ID should not be empty")
	}
}

func TestAssistantMessage(t *testing.T) {
	msg := AssistantMessage("Assistant response")

	if msg.Role != RoleAssistant {
		t.Errorf("AssistantMessage.Role = %v, want %v", msg.Role, RoleAssistant)
	}
	if len(msg.Parts) != 1 {
		t.Errorf("AssistantMessage.Parts length = %v, want 1", len(msg.Parts))
	}
	if msg.ID == "" {
		t.Errorf("AssistantMessage.ID should not be empty")
	}
}

func TestParts(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []interface{}
		expected int
	}{
		{
			"String inputs",
			[]interface{}{"hello", "world"},
			2,
		},
		{
			"Mixed inputs",
			[]interface{}{"hello", TextPart{Text: "world"}, FilePart{Name: "test.txt"}},
			3,
		},
		{
			"Empty inputs",
			[]interface{}{},
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert interface{} to the expected type for Parts function
			var parts []Part
			for _, input := range tt.inputs {
				switch v := input.(type) {
				case string:
					parts = append(parts, TextPart{Text: v})
				case TextPart:
					parts = append(parts, v)
				case FilePart:
					parts = append(parts, v)
				}
			}

			if len(parts) != tt.expected {
				t.Errorf("Parts length = %v, want %v", len(parts), tt.expected)
			}
		})
	}
}

func TestNewMessage(t *testing.T) {
	msg := NewMessage(RoleUser)

	if msg.Role != RoleUser {
		t.Errorf("NewMessage.Role = %v, want %v", msg.Role, RoleUser)
	}
	if msg.ID == "" {
		t.Errorf("NewMessage.ID should not be empty")
	}
	if len(msg.Parts) != 0 {
		t.Errorf("NewMessage.Parts should be empty")
	}
}

func TestNewMessageID(t *testing.T) {
	id1 := NewMessageID()
	id2 := NewMessageID()

	if id1 == "" {
		t.Errorf("NewMessageID() should not be empty")
	}
	if id2 == "" {
		t.Errorf("NewMessageID() should not be empty")
	}
	if id1 == id2 {
		t.Errorf("NewMessageID() should generate unique IDs")
	}
}
