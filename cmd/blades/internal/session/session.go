// Package session wraps blades.Session with file-based persistence.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kratos/blades"
)

// persistedSession is the JSON envelope stored on disk.
type persistedSession struct {
	ID        string             `json:"id"`
	CreatedAt time.Time          `json:"createdAt"`
	State     map[string]any     `json:"state"`
	History   []persistedMessage `json:"history,omitempty"`
}

type persistedMessage struct {
	ID           string            `json:"id"`
	Role         blades.Role       `json:"role"`
	Author       string            `json:"author"`
	InvocationID string            `json:"invocationId,omitempty"`
	Status       blades.Status     `json:"status"`
	FinishReason string            `json:"finishReason,omitempty"`
	TokenUsage   blades.TokenUsage `json:"tokenUsage,omitempty"`
	Actions      map[string]any    `json:"actions,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	Parts        []persistedPart   `json:"parts,omitempty"`
}

type persistedPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	URI       string          `json:"uri,omitempty"`
	MIMEType  blades.MIMEType `json:"mimeType,omitempty"`
	Bytes     []byte          `json:"bytes,omitempty"`
	ID        string          `json:"id,omitempty"`
	Request   string          `json:"arguments,omitempty"`
	Response  string          `json:"result,omitempty"`
	Completed bool            `json:"completed,omitempty"`
}

type managedSession struct {
	id   string
	base blades.Session
}

func (s *managedSession) ID() string { return s.id }

func (s *managedSession) State() blades.State { return s.base.State() }

func (s *managedSession) SetState(key string, value any) { s.base.SetState(key, value) }

func (s *managedSession) History() []*blades.Message { return s.base.History() }

func (s *managedSession) Append(ctx context.Context, message *blades.Message) error {
	return s.base.Append(ctx, message)
}

// Manager manages CLI sessions with optional file persistence.
type Manager struct {
	dir string
	mu  sync.Mutex
	// in-memory sessions keyed by id
	sessions map[string]blades.Session
}

// NewManager creates a Manager that persists sessions to dir.
func NewManager(dir string) *Manager {
	return &Manager{
		dir:      dir,
		sessions: make(map[string]blades.Session),
	}
}

// Get returns the existing in-memory session or loads one from disk.
// If no session is found a new one is created and persisted.
func (m *Manager) Get(id string) (blades.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, ok := m.sessions[id]; ok {
		return sess, nil
	}

	// Try loading from disk.
	sess, err := m.loadFromDisk(id)
	if err != nil {
		// Not found on disk — create fresh.
		sess = newManagedSession(id, nil, nil)
	}
	m.sessions[id] = sess
	return sess, nil
}

// GetOrNew returns the existing session for id, or a fresh unnamed session.
func (m *Manager) GetOrNew(id string) blades.Session {
	sess, _ := m.Get(id)
	return sess
}

// Save persists a session's state to disk.
func (m *Manager) Save(sess blades.Session) error {
	if m.dir == "" {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}

	ps := persistedSession{
		ID:        sess.ID(),
		CreatedAt: time.Now(),
		State:     sess.State(),
		History:   makePersistedHistory(sess.History()),
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}

	path := filepath.Join(m.dir, sess.ID()+".json")
	return os.WriteFile(path, data, 0o644)
}

// loadFromDisk reads a persisted session state.
func (m *Manager) loadFromDisk(id string) (blades.Session, error) {
	path := filepath.Join(m.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ps persistedSession
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("session: unmarshal %s: %w", id, err)
	}
	history, err := restoreHistory(ps.History)
	if err != nil {
		return nil, fmt.Errorf("session: decode history %s: %w", id, err)
	}
	return newManagedSession(id, ps.State, history), nil
}

func newManagedSession(id string, state map[string]any, history []*blades.Message) blades.Session {
	base := blades.NewSession(state)
	for _, message := range history {
		_ = base.Append(context.Background(), message)
	}
	return &managedSession{id: id, base: base}
}

func makePersistedHistory(history []*blades.Message) []persistedMessage {
	if len(history) == 0 {
		return nil
	}
	persisted := make([]persistedMessage, 0, len(history))
	for _, message := range history {
		if message == nil {
			continue
		}
		persisted = append(persisted, persistedMessage{
			ID:           message.ID,
			Role:         message.Role,
			Author:       message.Author,
			InvocationID: message.InvocationID,
			Status:       message.Status,
			FinishReason: message.FinishReason,
			TokenUsage:   message.TokenUsage,
			Actions:      message.Actions,
			Metadata:     message.Metadata,
			Parts:        makePersistedParts(message.Parts),
		})
	}
	return persisted
}

func makePersistedParts(parts []blades.Part) []persistedPart {
	if len(parts) == 0 {
		return nil
	}
	persisted := make([]persistedPart, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case blades.TextPart:
			persisted = append(persisted, persistedPart{Type: "text", Text: v.Text})
		case blades.FilePart:
			persisted = append(persisted, persistedPart{
				Type:     "file",
				Name:     v.Name,
				URI:      v.URI,
				MIMEType: v.MIMEType,
			})
		case blades.DataPart:
			persisted = append(persisted, persistedPart{
				Type:     "data",
				Name:     v.Name,
				Bytes:    v.Bytes,
				MIMEType: v.MIMEType,
			})
		case blades.ToolPart:
			persisted = append(persisted, persistedPart{
				Type:      "tool",
				ID:        v.ID,
				Name:      v.Name,
				Request:   v.Request,
				Response:  v.Response,
				Completed: v.Completed,
			})
		}
	}
	return persisted
}

func restoreHistory(history []persistedMessage) ([]*blades.Message, error) {
	if len(history) == 0 {
		return nil, nil
	}
	restored := make([]*blades.Message, 0, len(history))
	for _, message := range history {
		parts, err := restoreParts(message.Parts)
		if err != nil {
			return nil, err
		}
		restored = append(restored, &blades.Message{
			ID:           message.ID,
			Role:         message.Role,
			Author:       message.Author,
			InvocationID: message.InvocationID,
			Status:       message.Status,
			FinishReason: message.FinishReason,
			TokenUsage:   message.TokenUsage,
			Actions:      message.Actions,
			Metadata:     message.Metadata,
			Parts:        parts,
		})
	}
	return restored, nil
}

func restoreParts(parts []persistedPart) ([]blades.Part, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	restored := make([]blades.Part, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			restored = append(restored, blades.TextPart{Text: part.Text})
		case "file":
			restored = append(restored, blades.FilePart{
				Name:     part.Name,
				URI:      part.URI,
				MIMEType: part.MIMEType,
			})
		case "data":
			restored = append(restored, blades.DataPart{
				Name:     part.Name,
				Bytes:    part.Bytes,
				MIMEType: part.MIMEType,
			})
		case "tool":
			restored = append(restored, blades.ToolPart{
				ID:        part.ID,
				Name:      part.Name,
				Request:   part.Request,
				Response:  part.Response,
				Completed: part.Completed,
			})
		default:
			return nil, fmt.Errorf("unknown message part type %q", part.Type)
		}
	}
	return restored, nil
}
