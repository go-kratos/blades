// Package logger provides runtime logging to the blades log directory (~/.blades/logs/).
// Log entries are written to YYYY-MM-DD.log; use memory.Store for conversation
// persistence to workspace/memory/ when logConversation is enabled.
package logger

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Runtime writes runtime/audit log lines to ~/.blades/logs/YYYY-MM-DD.log.
// It is safe for concurrent use.
type Runtime struct {
	homeDir string
}

// NewRuntime returns a Runtime that writes to logDir (e.g. ~/.blades/logs).
func NewRuntime(logDir string) *Runtime {
	return &Runtime{homeDir: logDir}
}

// Write appends a single log line (with newline) to today's log file.
func (r *Runtime) Write(format string, args ...any) {
	dir := filepath.Join(r.homeDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	log.New(f, "", 0).Printf(format, args...)
}

// WriteConversation logs a conversation event (session, role, text) for audit.
// Use this for channel/daemon message audit; use memory.Store.AppendDailyLog
// for persistent conversation memory when logConversation is true.
func (r *Runtime) WriteConversation(sessionID, role, text string) {
	r.Write("[%s] session=%s role=%s", time.Now().Format("2006-01-02 15:04:05"), sessionID, role)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if line != "" {
			r.Write("  %s", line)
		}
	}
	r.Write("")
}
