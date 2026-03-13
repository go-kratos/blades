// Package session wraps blades.Session with file-based persistence.
package session

import (
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
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	State     map[string]any `json:"state"`
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
		sess = blades.NewSession()
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
	sess := blades.NewSession(ps.State)
	return sess, nil
}
